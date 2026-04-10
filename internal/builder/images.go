package builder

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// extractedImage captures a single image reference plus the file it came
// from, which is useful for diagnostics when a bundle includes dozens of
// charts and something goes wrong.
type extractedImage struct {
	Ref    string
	Source string
}

// imageRefPattern matches strings that look like container image
// references: registry/org/name[:tag][@sha256:...]. This is intentionally
// permissive — callers must further validate the result (e.g. by
// attempting to pull it).
var imageRefPattern = regexp.MustCompile(
	`^[a-z0-9.\-]+(?::\d+)?/[a-z0-9][a-z0-9._\-/]*(?::[A-Za-z0-9._\-]+)?(?:@sha256:[a-fA-F0-9]{64})?$`,
)

// ExtractImagesFromChart walks a helm chart directory and returns all
// container image references discovered in values.yaml files.
//
// Recognized patterns (all nested anywhere in the YAML tree):
//  1. `image: "registry/repo:tag"` — plain string
//  2. `image: { repository: "...", tag: "..." }` — structured
//  3. `image: { registry: "...", repository: "...", tag: "..." }` — structured with registry
//  4. `images: { repository: "...", tags: { k1: "full-ref-or-tag", k2: "..." } }` — SD-Core style
//
// The returned slice is sorted and deduplicated.
func ExtractImagesFromChart(chartDir string) ([]string, error) {
	var extracted []extractedImage

	err := filepath.WalkDir(chartDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if name != "values.yaml" && name != "values.yml" {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}

		var root yaml.Node
		if err := yaml.Unmarshal(data, &root); err != nil {
			// Skip unparseable files — helm charts sometimes ship template
			// fragments in files named values.yaml for documentation purposes.
			return nil
		}

		refs := walkForImages(&root)
		for _, r := range refs {
			extracted = append(extracted, extractedImage{Ref: r, Source: path})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	var out []string
	for _, e := range extracted {
		if seen[e.Ref] {
			continue
		}
		seen[e.Ref] = true
		out = append(out, e.Ref)
	}
	sort.Strings(out)
	return out, nil
}

// walkForImages recursively searches a YAML tree for image references,
// applying the structured patterns described on ExtractImagesFromChart.
func walkForImages(n *yaml.Node) []string {
	if n == nil {
		return nil
	}

	var out []string
	switch n.Kind {
	case yaml.DocumentNode:
		for _, c := range n.Content {
			out = append(out, walkForImages(c)...)
		}
	case yaml.SequenceNode:
		for _, c := range n.Content {
			out = append(out, walkForImages(c)...)
		}
	case yaml.MappingNode:
		// First, check whether this mapping itself represents an image
		// descriptor (image: {...} or images: {...}).
		out = append(out, imagesFromMapping(n)...)

		// Recurse into every value regardless — nested charts / subsections
		// can contain image descriptors at arbitrary depths.
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := n.Content[i]
			val := n.Content[i+1]

			// Plain `image: "registry/repo:tag"` form.
			if val.Kind == yaml.ScalarNode && (key.Value == "image") {
				if ref := normalizeImageRef(val.Value); ref != "" {
					out = append(out, ref)
				}
			}

			out = append(out, walkForImages(val)...)
		}
	}
	return out
}

// imagesFromMapping inspects a mapping node that may itself describe one
// or more images (the `image:` or `images:` subtree value). It returns
// every image ref it can assemble from the mapping's children.
func imagesFromMapping(n *yaml.Node) []string {
	if n.Kind != yaml.MappingNode {
		return nil
	}

	var out []string
	for i := 0; i+1 < len(n.Content); i += 2 {
		key := n.Content[i]
		val := n.Content[i+1]
		if key.Kind != yaml.ScalarNode {
			continue
		}

		switch key.Value {
		case "image":
			if val.Kind == yaml.MappingNode {
				if ref := imageFromDescriptor(val); ref != "" {
					out = append(out, ref)
				}
			}
		case "images":
			if val.Kind == yaml.MappingNode {
				out = append(out, imagesFromSDCoreDescriptor(val)...)
			}
		}
	}
	return out
}

// imageFromDescriptor assembles "registry/repository:tag" from a mapping
// like { registry: "docker.io", repository: "grafana/grafana", tag: "10.0.0" }.
// Missing registry defaults to the registry implied by the repository.
//
// Entries without an explicit tag are rejected. Airgap bundles must not
// contain non-deterministic `:latest` references — the operator has to
// pin a version, either by adding a tag in the chart values or listing
// the image explicitly in `images.extra`.
func imageFromDescriptor(n *yaml.Node) string {
	fields := scalarFields(n)
	repo := fields["repository"]
	tag := fields["tag"]
	registry := fields["registry"]

	if repo == "" {
		return ""
	}
	// Reject descriptors without an explicit tag. Without a tag we would
	// default to `:latest` at pull time, which is a silent footgun for
	// airgap installs.
	if tag == "" && !strings.Contains(repo, ":") {
		return ""
	}

	full := repo
	if registry != "" && !strings.Contains(repo, "/") {
		full = registry + "/" + repo
	} else if registry != "" && !strings.HasPrefix(repo, registry+"/") && !strings.Contains(strings.SplitN(repo, "/", 2)[0], ".") {
		full = registry + "/" + repo
	}

	if tag != "" && !strings.Contains(full, ":") {
		full = full + ":" + tag
	}
	return normalizeImageRef(full)
}

// imagesFromSDCoreDescriptor handles the SD-Core convention:
//
//	images:
//	  repository: "ghcr.io/omec-project/"  # registry prefix, trailing slash
//	  tags:
//	    amf: 5gc-amf:rel-2.1.1             # image-name:version
//	    pfcpiface: upf-pfcpiface:rel-2.2.0
//	  pullPolicy: Always
//
// Each tag value is concatenated directly onto `repository` — the
// convention is that repository is a trailing-slash prefix and the tag
// value carries the image name and version. Tag values that already
// contain a registry hostname (detected by a dot before the first slash)
// are taken verbatim, which handles the mixed `quay.io/...` entries that
// appear in some SD-Core charts.
func imagesFromSDCoreDescriptor(n *yaml.Node) []string {
	fields := scalarFields(n)
	repo := fields["repository"]

	var out []string
	for i := 0; i+1 < len(n.Content); i += 2 {
		key := n.Content[i]
		val := n.Content[i+1]
		if key.Kind != yaml.ScalarNode || key.Value != "tags" {
			continue
		}
		if val.Kind != yaml.MappingNode {
			continue
		}
		for j := 0; j+1 < len(val.Content); j += 2 {
			tagVal := val.Content[j+1]
			if tagVal.Kind != yaml.ScalarNode || tagVal.Value == "" {
				continue
			}
			raw := strings.TrimSpace(tagVal.Value)

			var candidate string
			if looksLikeFullRefWithRegistry(raw) {
				candidate = raw
			} else {
				candidate = repo + raw
			}
			if ref := normalizeImageRef(candidate); ref != "" {
				out = append(out, ref)
			}
		}
	}
	return out
}

// looksLikeFullRefWithRegistry reports whether s already starts with a
// registry hostname (something like "quay.io/..." or "docker.io/..."),
// meaning it should not be prefixed with a sibling `repository` value.
// The heuristic: there is a "/" and the segment before it contains a dot
// or a colon (port).
func looksLikeFullRefWithRegistry(s string) bool {
	slash := strings.Index(s, "/")
	if slash <= 0 {
		return false
	}
	first := s[:slash]
	return strings.ContainsAny(first, ".:")
}

// scalarFields collects the scalar key/value pairs directly on a mapping
// node. Nested mappings are ignored — those are the caller's concern.
func scalarFields(n *yaml.Node) map[string]string {
	out := map[string]string{}
	if n == nil || n.Kind != yaml.MappingNode {
		return out
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k := n.Content[i]
		v := n.Content[i+1]
		if k.Kind == yaml.ScalarNode && v.Kind == yaml.ScalarNode {
			out[k.Value] = v.Value
		}
	}
	return out
}

// normalizeImageRef trims and validates a candidate image reference.
// Returns empty string if the input does not look like a valid reference.
func normalizeImageRef(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Reject helm template expressions — these are not resolvable at
	// build time and the operator must lock them down in the spec.
	if strings.Contains(s, "{{") {
		return ""
	}
	if !imageRefPattern.MatchString(s) {
		return ""
	}
	return s
}
