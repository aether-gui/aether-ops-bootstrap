// Package releaseyaml mutates site/releases.yaml in place to promote a
// freshly-built release to current and demote the previous current
// release to external.
//
// The release pipeline lives in two places: cmd/build-release-site
// reads releases.yaml to render the public site, and the deploy
// workflow needs to rotate the YAML each release. Doing the rotation
// in Go (rather than bash + jq + sed) lets the operation round-trip
// through `gopkg.in/yaml.v3`'s Node API, which preserves comments and
// most formatting and is unit-testable against golden fixtures.
//
// The promotion is a two-step edit on the `releases:` sequence:
//
//  1. The existing entry with `current: true` is rewritten in place —
//     `current` is removed, `external: true` is added, and any
//     `source:` / `sha256_source:` fields on the bootstrap, bundle,
//     and patch_tool blocks are replaced with literal `sha256:` values
//     captured from the caller (typically pulled from the live site's
//     metadata.json just before the rebuild overwrites dist/).
//
//  2. A new entry is inserted at the top of `releases:` with
//     `current: true`, paths under `<version>/`, source-from-dist
//     fields, the build commit, the components list pulled from
//     `specs/bundle.yaml`, and the per-artifact release notes provided
//     by the caller.
package releaseyaml

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"time"

	"gopkg.in/yaml.v3"
)

// PriorSHAs holds the inlined SHA256 values to attach to the
// previous-current release when it is demoted to external. All three
// fields are required — every shipped release has a bootstrap, a
// bundle, and a patch_tool.
type PriorSHAs struct {
	Bootstrap string
	Bundle    string
	PatchTool string
}

// Options drives one Promote call.
type Options struct {
	// YAMLPath is the path to releases.yaml on disk.
	YAMLPath string
	// SpecPath is the path to specs/bundle.yaml. Components for the
	// new entry are read from here (aether_ops.version, onramp.ref,
	// rke2.version, helm.version, helm_charts[*].ref).
	SpecPath string
	// NewVersion is the calver-style version string for the new
	// release (e.g. "2026.05.11.1"). Used for path: and version:
	// fields on every artifact.
	NewVersion string
	// ID is the human-readable slug used for the new release entry's
	// `id:` field, e.g. "2026-05-11-aether-ops-v0.2.0". Optional; a
	// sensible default is generated when empty.
	ID string
	// PublishedAt is the timestamp written to the new entry. Zero
	// value defaults to time.Now().UTC().
	PublishedAt time.Time
	// BuildCommit is the short git SHA the release was built from. It
	// is written to bootstrap.commit, bundle.build_commit, and
	// patch_tool.build_commit on the new entry.
	BuildCommit string
	// BootstrapNotes / BundleNotes / PatchToolNotes are the
	// release_notes bodies written verbatim into the new entry. They
	// may be multi-line; the YAML encoder emits them as literal
	// blocks. Empty strings emit an empty release_notes: field, which
	// is acceptable but operators normally want at least a sentence.
	BootstrapNotes string
	BundleNotes    string
	PatchToolNotes string
	// Prior captures the SHA256s of the previous-current release's
	// three artifacts. Required.
	Prior PriorSHAs
}

// Promote applies the rotation described in opts to opts.YAMLPath in
// place. It returns an error if no current release exists, if the
// spec cannot be read for components, or if the file fails to write
// back. On error the file is left untouched.
func Promote(opts Options) error {
	if opts.YAMLPath == "" {
		return errors.New("releaseyaml: YAMLPath is required")
	}
	if opts.NewVersion == "" {
		return errors.New("releaseyaml: NewVersion is required")
	}
	if opts.Prior.Bootstrap == "" || opts.Prior.Bundle == "" || opts.Prior.PatchTool == "" {
		return errors.New("releaseyaml: all three Prior SHAs are required (bootstrap, bundle, patch_tool)")
	}
	if opts.BuildCommit == "" {
		return errors.New("releaseyaml: BuildCommit is required")
	}

	publishedAt := opts.PublishedAt
	if publishedAt.IsZero() {
		publishedAt = time.Now().UTC()
	}
	id := opts.ID
	if id == "" {
		id = fmt.Sprintf("release-%s", opts.NewVersion)
	}

	data, err := os.ReadFile(opts.YAMLPath)
	if err != nil {
		return fmt.Errorf("releaseyaml: reading %s: %w", opts.YAMLPath, err)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("releaseyaml: parsing %s: %w", opts.YAMLPath, err)
	}

	doc := documentRoot(&root)
	if doc == nil || doc.Kind != yaml.MappingNode {
		return fmt.Errorf("releaseyaml: %s does not have a mapping at the root", opts.YAMLPath)
	}

	releasesNode := lookupMapValue(doc, "releases")
	if releasesNode == nil || releasesNode.Kind != yaml.SequenceNode {
		return fmt.Errorf("releaseyaml: %s missing or invalid `releases:` sequence", opts.YAMLPath)
	}

	if err := demoteCurrent(releasesNode, opts.Prior); err != nil {
		return err
	}

	components, err := readComponents(opts.SpecPath)
	if err != nil {
		return fmt.Errorf("releaseyaml: reading spec components: %w", err)
	}

	newEntry, err := buildNewEntry(id, opts.NewVersion, publishedAt, opts.BuildCommit,
		opts.BootstrapNotes, opts.BundleNotes, opts.PatchToolNotes, components)
	if err != nil {
		return err
	}

	// Prepend the new entry at the top of `releases:`.
	releasesNode.Content = append([]*yaml.Node{newEntry}, releasesNode.Content...)

	out, err := marshalDoc(&root)
	if err != nil {
		return fmt.Errorf("releaseyaml: marshaling updated YAML: %w", err)
	}

	if err := os.WriteFile(opts.YAMLPath, out, 0o644); err != nil {
		return fmt.Errorf("releaseyaml: writing %s: %w", opts.YAMLPath, err)
	}
	return nil
}

// documentRoot returns the mapping node nested under a yaml.Node tree
// returned by yaml.Unmarshal. A "top-level" Node from Unmarshal is a
// DocumentNode wrapping the real root; tests sometimes pass the
// mapping directly. Tolerate both shapes.
func documentRoot(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		return n.Content[0]
	}
	return n
}

// lookupMapValue finds the value node for a given key in a mapping.
// Returns nil when missing.
func lookupMapValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

// removeMapKey drops the (key, value) pair from a mapping. No-op when
// the key is not present.
func removeMapKey(mapping *yaml.Node, key string) {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content = append(mapping.Content[:i], mapping.Content[i+2:]...)
			return
		}
	}
}

// setMapKey inserts or overwrites (key, scalar) on a mapping. Existing
// entries keep their position; new entries are appended.
func setMapKey(mapping *yaml.Node, key string, scalar *yaml.Node) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content[i+1] = scalar
			return
		}
	}
	mapping.Content = append(mapping.Content, scalarNode(key), scalar)
}

// demoteCurrent finds the single release entry with `current: true`
// and rewrites it for external hosting. Returns an error if zero or
// more than one current entry is found — the rest of the rotation is
// only meaningful when there is exactly one outgoing release.
func demoteCurrent(releases *yaml.Node, prior PriorSHAs) error {
	var found *yaml.Node
	count := 0
	for _, entry := range releases.Content {
		if entry.Kind != yaml.MappingNode {
			continue
		}
		v := lookupMapValue(entry, "current")
		if v == nil {
			continue
		}
		if v.Kind == yaml.ScalarNode && (v.Value == "true" || v.Value == "True" || v.Value == "TRUE") {
			found = entry
			count++
		}
	}
	if count == 0 {
		return errors.New("releaseyaml: no release with `current: true` to demote")
	}
	if count > 1 {
		return errors.New("releaseyaml: more than one release marked `current: true`")
	}

	removeMapKey(found, "current")
	setMapKey(found, "external", scalarBool(true))

	if err := demoteArtifact(lookupMapValue(found, "bootstrap"), prior.Bootstrap); err != nil {
		return fmt.Errorf("demoting bootstrap: %w", err)
	}
	if err := demoteArtifact(lookupMapValue(found, "bundle"), prior.Bundle); err != nil {
		return fmt.Errorf("demoting bundle: %w", err)
	}
	if err := demoteArtifact(lookupMapValue(found, "patch_tool"), prior.PatchTool); err != nil {
		return fmt.Errorf("demoting patch_tool: %w", err)
	}
	return nil
}

// demoteArtifact rewrites one of the bootstrap/bundle/patch_tool
// blocks on a release so its artifacts are described by a literal
// `sha256:` field instead of `source:` / `sha256_source:` pointers
// into ../dist (which would silently follow the freshly-rebuilt
// artifacts after the next build wipes dist/).
func demoteArtifact(artifact *yaml.Node, sha string) error {
	if artifact == nil {
		return errors.New("artifact block missing")
	}
	if artifact.Kind != yaml.MappingNode {
		return errors.New("artifact block is not a mapping")
	}
	removeMapKey(artifact, "source")
	removeMapKey(artifact, "sha256_source")
	setMapKey(artifact, "sha256", scalarString(sha))
	return nil
}

// scalarNode returns a YAML scalar with the given string value.
func scalarNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Value: value}
}

// scalarString returns a YAML scalar styled as a double-quoted
// string. Used for SHA256 hex strings and similar fixed-length tokens
// where we want the value to round-trip through other tools
// (especially jq) without ambiguity.
func scalarString(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Style: yaml.DoubleQuotedStyle, Value: value}
}

// scalarBool returns a YAML scalar for a Go bool.
func scalarBool(v bool) *yaml.Node {
	if v {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"}
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "false"}
}

// scalarLiteralBlock returns a YAML scalar emitted with `|` literal
// block style (newlines preserved). Used for release_notes bodies so
// the YAML stays human-readable.
func scalarLiteralBlock(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Style: yaml.LiteralStyle, Value: value}
}

// marshalDoc round-trips a yaml.Node through the v3 encoder with the
// same 2-space indent the existing releases.yaml uses.
func marshalDoc(root *yaml.Node) ([]byte, error) {
	var buf yamlBuffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		_ = enc.Close()
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// yamlBuffer is a tiny bytes.Buffer-shaped writer that avoids
// importing bytes just for one type. Implements io.Writer.
type yamlBuffer struct {
	b []byte
}

func (y *yamlBuffer) Write(p []byte) (int, error) {
	y.b = append(y.b, p...)
	return len(p), nil
}
func (y *yamlBuffer) Bytes() []byte { return y.b }

// specComponents is the minimal projection of specs/bundle.yaml the
// release entry needs.
type specComponents struct {
	AetherOpsVersion string
	OnrampRef        string
	RKE2Version      string
	HelmVersion      string
	HelmCharts       []helmChartRef
}

type helmChartRef struct {
	Name string
	Ref  string
}

// readComponents parses the relevant fields of specs/bundle.yaml.
// Only the keys release notes care about are touched; the rest of
// the spec is ignored.
func readComponents(path string) (specComponents, error) {
	var s specComponents
	if path == "" {
		return s, errors.New("SpecPath is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	var raw struct {
		AetherOps *struct {
			Version string `yaml:"version"`
		} `yaml:"aether_ops"`
		Onramp *struct {
			Ref string `yaml:"ref"`
		} `yaml:"onramp"`
		RKE2 *struct {
			Version string `yaml:"version"`
		} `yaml:"rke2"`
		Helm *struct {
			Version string `yaml:"version"`
		} `yaml:"helm"`
		HelmCharts []struct {
			Name string `yaml:"name"`
			Ref  string `yaml:"ref"`
		} `yaml:"helm_charts"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return s, err
	}
	if raw.AetherOps != nil {
		s.AetherOpsVersion = raw.AetherOps.Version
	}
	if raw.Onramp != nil {
		s.OnrampRef = raw.Onramp.Ref
	}
	if raw.RKE2 != nil {
		s.RKE2Version = raw.RKE2.Version
	}
	if raw.Helm != nil {
		s.HelmVersion = raw.Helm.Version
	}
	for _, hc := range raw.HelmCharts {
		s.HelmCharts = append(s.HelmCharts, helmChartRef{Name: hc.Name, Ref: hc.Ref})
	}
	sort.Slice(s.HelmCharts, func(i, j int) bool { return s.HelmCharts[i].Name < s.HelmCharts[j].Name })
	return s, nil
}

// buildNewEntry constructs the YAML mapping node for the new release.
// Order is fixed (id, published_at, current, bootstrap, bundle,
// patch_tool) to match the hand-edited convention in
// site/releases.yaml.
func buildNewEntry(id, version string, publishedAt time.Time, buildCommit, bootstrapNotes, bundleNotes, patchToolNotes string, c specComponents) (*yaml.Node, error) {
	entry := &yaml.Node{Kind: yaml.MappingNode}

	setMapKey(entry, "id", scalarString(id))
	setMapKey(entry, "published_at", scalarString(publishedAt.Format(time.RFC3339)))
	setMapKey(entry, "current", scalarBool(true))

	setMapKey(entry, "bootstrap", buildArtifactBlock(artifactInputs{
		Version:     version,
		Filename:    "aether-ops-bootstrap",
		Source:      "../dist/aether-ops-bootstrap",
		CommitKey:   "commit",
		BuildCommit: buildCommit,
		Notes:       bootstrapNotes,
	}))

	bundleBlock := buildArtifactBlock(artifactInputs{
		Version:      version,
		Filename:     "bundle.tar.zst",
		Source:       "../dist/bundle.tar.zst",
		SHA256Source: "../dist/bundle.tar.zst.sha256",
		CommitKey:    "build_commit",
		BuildCommit:  buildCommit,
		Notes:        bundleNotes,
	})
	setMapKey(bundleBlock, "components", buildComponentsNode(c))
	setMapKey(entry, "bundle", bundleBlock)

	setMapKey(entry, "patch_tool", buildArtifactBlock(artifactInputs{
		Version:     version,
		Filename:    "patch-bundle",
		Source:      "../dist/patch-bundle",
		CommitKey:   "build_commit",
		BuildCommit: buildCommit,
		Notes:       patchToolNotes,
	}))

	return entry, nil
}

type artifactInputs struct {
	Version      string
	Filename     string
	Source       string
	SHA256Source string // empty when the artifact has no .sha256 sidecar
	CommitKey    string // "commit" for bootstrap, "build_commit" for bundle and patch_tool
	BuildCommit  string
	Notes        string
}

func buildArtifactBlock(in artifactInputs) *yaml.Node {
	m := &yaml.Node{Kind: yaml.MappingNode}
	setMapKey(m, "version", scalarString(in.Version))
	setMapKey(m, "path", scalarString(in.Version))
	setMapKey(m, "filename", scalarNode(in.Filename))
	setMapKey(m, "source", scalarNode(in.Source))
	if in.SHA256Source != "" {
		setMapKey(m, "sha256_source", scalarNode(in.SHA256Source))
	}
	setMapKey(m, in.CommitKey, scalarString(in.BuildCommit))
	if in.Notes != "" {
		setMapKey(m, "release_notes", scalarLiteralBlock(in.Notes))
	} else {
		setMapKey(m, "release_notes", scalarLiteralBlock("TODO: fill in release notes before merging.\n"))
	}
	return m
}

func buildComponentsNode(c specComponents) *yaml.Node {
	seq := &yaml.Node{Kind: yaml.SequenceNode}
	add := func(name, version, commit string) {
		if version == "" && commit == "" {
			return
		}
		m := &yaml.Node{Kind: yaml.MappingNode}
		setMapKey(m, "name", scalarNode(name))
		if version != "" {
			setMapKey(m, "version", scalarNode(version))
		}
		if commit != "" {
			setMapKey(m, "commit", scalarNode(commit))
		}
		seq.Content = append(seq.Content, m)
	}
	add("aether-ops", c.AetherOpsVersion, "")
	add("aether-onramp", "", c.OnrampRef)
	add("rke2", c.RKE2Version, "")
	add("helm", c.HelmVersion, "")
	for _, hc := range c.HelmCharts {
		add(hc.Name, hc.Ref, "")
	}
	return seq
}
