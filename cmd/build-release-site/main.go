package main

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aether-gui/aether-ops-bootstrap/internal/releaseyaml"
	"gopkg.in/yaml.v3"
)

//go:embed assets/*.css assets/*.tmpl
var assetsFS embed.FS

type siteMetadata struct {
	SchemaVersion int             `yaml:"schema_version"`
	Site          siteConfig      `yaml:"site"`
	Releases      []releaseConfig `yaml:"releases"`
}

type siteConfig struct {
	Title         string                          `yaml:"title" json:"title"`
	BaseURLPath   string                          `yaml:"base_url_path" json:"base_url_path"`
	Description   string                          `yaml:"description" json:"description"`
	ArtifactKinds map[string]artifactKindDefaults `yaml:"artifact_kinds,omitempty" json:"artifact_kinds,omitempty"`
}

// artifactKindDefaults provides static metadata about an artifact kind
// (bootstrap / bundle / patch_tool) — what it is, where to find docs.
// Per-release artifact entries inherit Description and DocsURL from the
// matching kind so blurbs aren't duplicated across releases.
type artifactKindDefaults struct {
	Label       string `yaml:"label,omitempty" json:"label,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	DocsURL     string `yaml:"docs_url,omitempty" json:"docs_url,omitempty"`
}

type releaseConfig struct {
	ID          string          `yaml:"id"`
	PublishedAt string          `yaml:"published_at"`
	Current     bool            `yaml:"current"`
	External    bool            `yaml:"external"`
	Bootstrap   artifactConfig  `yaml:"bootstrap"`
	Bundle      artifactConfig  `yaml:"bundle"`
	PatchTool   *artifactConfig `yaml:"patch_tool,omitempty"`
}

type artifactConfig struct {
	Label        string `yaml:"label"`
	Version      string `yaml:"version"`
	Path         string `yaml:"path"`
	Filename     string `yaml:"filename"`
	Source       string `yaml:"source"`
	SHA256       string `yaml:"sha256"`
	SHA256Source string `yaml:"sha256_source"`
	Commit       string `yaml:"commit"`
	BuildCommit  string `yaml:"build_commit"`
	// ReleaseSummary is a one- or two-sentence headline shown above the
	// (collapsible) bullet list. Optional — when empty the page shows
	// the bullet list with no preamble. Authored as a YAML scalar so
	// long sentences wrap naturally in the source file.
	ReleaseSummary string            `yaml:"release_summary,omitempty"`
	ReleaseNotes   []string          `yaml:"release_notes"`
	Components     []componentConfig `yaml:"components"`
	// SecurityArtifacts are the supply-chain sidecars (SBOM, Grype
	// scan, OpenVEX doc) published next to the primary artifact under
	// the same per-version path. Optional — releases that predate the
	// SBOM/Grype/VEX pipeline omit this list. When present, each entry
	// is materialised exactly like the primary artifact: copied into
	// the output tree, hashed, with a .sha256 sidecar written next to
	// it.
	SecurityArtifacts []securityArtifactConfig `yaml:"security_artifacts,omitempty"`
}

// securityArtifactConfig is one supply-chain sidecar attached to an
// artifact block. Kind is free-form ("sbom", "grype", "vex") and only
// drives the label fallback and link styling; the renderer treats the
// list as opaque downloads otherwise.
type securityArtifactConfig struct {
	Kind     string `yaml:"kind"`
	Label    string `yaml:"label,omitempty"`
	Filename string `yaml:"filename"`
	Source   string `yaml:"source,omitempty"`
	SHA256   string `yaml:"sha256,omitempty"`
}

type componentConfig struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
	Commit  string `yaml:"commit"`
}

type publicMetadata struct {
	SchemaVersion int             `json:"schema_version"`
	Site          siteConfig      `json:"site"`
	Releases      []publicRelease `json:"releases"`
}

type publicRelease struct {
	ID          string          `json:"id"`
	PublishedAt string          `json:"published_at"`
	Current     bool            `json:"current"`
	External    bool            `json:"external,omitempty"`
	Bootstrap   publicArtifact  `json:"bootstrap"`
	Bundle      publicArtifact  `json:"bundle"`
	PatchTool   *publicArtifact `json:"patch_tool,omitempty"`
}

type publicArtifact struct {
	Kind              string                   `json:"kind"` // "bootstrap" | "bundle" | "patch_tool"
	Label             string                   `json:"label"`
	Description       string                   `json:"description,omitempty"`
	DocsURL           string                   `json:"docs_url,omitempty"`
	Version           string                   `json:"version"`
	Path              string                   `json:"path"`
	Filename          string                   `json:"filename"`
	SHA256            string                   `json:"sha256"`
	Commit            string                   `json:"commit,omitempty"`
	BuildCommit       string                   `json:"build_commit,omitempty"`
	ReleaseSummary    string                   `json:"release_summary,omitempty"`
	ReleaseNotes      []string                 `json:"release_notes,omitempty"`
	URL               string                   `json:"url"`
	SHA256URL         string                   `json:"sha256_url,omitempty"`
	Components        []publicComponent        `json:"components,omitempty"`
	SecurityArtifacts []publicSecurityArtifact `json:"security_artifacts,omitempty"`
}

type publicSecurityArtifact struct {
	Kind      string `json:"kind"`
	Label     string `json:"label"`
	Filename  string `json:"filename"`
	SHA256    string `json:"sha256"`
	URL       string `json:"url"`
	SHA256URL string `json:"sha256_url"`
}

type publicComponent struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	Commit  string `json:"commit,omitempty"`
}

type renderedSite struct {
	Title       string
	Description string
	BaseURLPath string
	Latest      renderedRelease
	Releases    []renderedRelease
}

type renderedRelease struct {
	ID          string
	PublishedAt string
	Current     bool
	External    bool
	Bootstrap   renderedArtifact
	Bundle      renderedArtifact
	PatchTool   *renderedArtifact // nil when the release does not ship a patch tool
}

type renderedArtifact struct {
	Kind              string // "bootstrap" | "bundle" | "patch_tool"
	Label             string
	Description       string
	DocsURL           string
	Version           string
	Path              string
	Filename          string
	SHA256            string
	Commit            string
	BuildCommit       string
	ReleaseSummary    string
	ReleaseNotes      []string
	URL               string
	SHA256URL         string
	Components        []renderedComponent
	SecurityArtifacts []renderedSecurityArtifact
}

type renderedSecurityArtifact struct {
	Kind      string
	Label     string
	Filename  string
	SHA256    string
	URL       string
	SHA256URL string
}

type renderedComponent struct {
	Name    string
	Version string
	Commit  string
}

func main() {
	var metadataPath string
	var outputDir string
	var promoteVersion string
	var promoteID string
	var promoteSpec string
	var promoteCommit string
	var priorBootstrapSHA string
	var priorBundleSHA string
	var priorPatchToolSHA string
	var bootstrapNotesFile string
	var bundleNotesFile string
	var patchToolNotesFile string

	flag.StringVar(&metadataPath, "metadata", "", "path to release metadata YAML")
	flag.StringVar(&outputDir, "output", "", "output directory for generated site")

	// --- release rotation (optional) ---
	// When --promote-current is set, the metadata YAML is rewritten in
	// place BEFORE the site is rendered: the existing current release
	// is demoted to external with SHAs from the --prior-*-sha flags,
	// and a new release entry is inserted at the top using
	// --build-commit and components from --spec.
	flag.StringVar(&promoteVersion, "promote-current", "", "new release version to promote to current (e.g. 2026.05.11.1); when set, releases.yaml is rotated before rendering")
	flag.StringVar(&promoteID, "promote-id", "", "human-readable slug for the new release entry's id field (default: release-<version>)")
	flag.StringVar(&promoteSpec, "spec", "specs/bundle.yaml", "path to specs/bundle.yaml (components list source) when promoting")
	flag.StringVar(&promoteCommit, "build-commit", "", "short git SHA the artifacts were built from (required with --promote-current)")
	flag.StringVar(&priorBootstrapSHA, "prior-bootstrap-sha", "", "SHA256 of the outgoing current release's bootstrap, inlined when demoting it (required with --promote-current)")
	flag.StringVar(&priorBundleSHA, "prior-bundle-sha", "", "SHA256 of the outgoing current release's bundle (required with --promote-current)")
	flag.StringVar(&priorPatchToolSHA, "prior-patch-tool-sha", "", "SHA256 of the outgoing current release's patch_tool (required with --promote-current)")
	flag.StringVar(&bootstrapNotesFile, "bootstrap-notes", "", "path to a markdown/text file used verbatim as the new bootstrap release_notes (default: placeholder text)")
	flag.StringVar(&bundleNotesFile, "bundle-notes", "", "path to a markdown/text file used verbatim as the new bundle release_notes (default: placeholder text)")
	flag.StringVar(&patchToolNotesFile, "patch-tool-notes", "", "path to a markdown/text file used verbatim as the new patch_tool release_notes (default: placeholder text)")

	// Retention: when --prune-keep is set to a non-negative value,
	// external release entries beyond the first N are dropped from
	// releases.yaml after promotion. The list of pruned versions is
	// printed to stdout so a wrapping shell/workflow can delete the
	// matching on-disk artifact directories. Default of -1 means
	// "no pruning" so existing call sites keep their behaviour.
	pruneKeep := -1
	flag.IntVar(&pruneKeep, "prune-keep", -1, "after promotion, prune external release entries beyond the first N (e.g. --prune-keep=2 keeps current + 2 prior). Set to -1 (default) to disable.")

	// --- rerender-only mode (optional) ---
	// When --keep-existing-artifacts is set the renderer treats
	// --output as the live published tree: it does NOT wipe the
	// directory at startup, and for non-external releases it reads
	// each artifact's SHA256 from the pre-existing .sha256 sidecar
	// instead of copying the artifact from --source and rehashing.
	// Use this to refresh the HTML pages, CSS, and metadata.json
	// against an already-published artifact tree without re-uploading
	// the (multi-GB) bundles.
	var keepArtifacts bool
	flag.BoolVar(&keepArtifacts, "keep-existing-artifacts", false, "skip artifact copy/hash and read SHA256 from the pre-existing <output>/<kind>/<path>/<filename>.sha256 sidecar; do not wipe --output. Used to re-render index pages against a live artifact tree.")

	// Repeatable: --prior-security-sha=<parent_kind>:<filename>=<hex>
	// e.g. --prior-security-sha=bundle:openvex.json=abc123…
	// One flag per supply-chain sidecar present on the outgoing
	// current release's security_artifacts list. Demotion fails loudly
	// when a present entry is missing a SHA here.
	var priorSecuritySHAs priorSecuritySHAFlag
	flag.Var(&priorSecuritySHAs, "prior-security-sha", "repeatable SHA256 for one supply-chain sidecar on the outgoing current release, formatted as <parent_kind>:<filename>=<hex> (parent_kind is bootstrap, bundle, or patch_tool)")

	flag.Parse()

	if metadataPath == "" || outputDir == "" {
		flag.Usage()
		os.Exit(2)
	}

	if promoteVersion != "" {
		if err := promoteRelease(metadataPath, promoteSpec, promoteVersion, promoteID, promoteCommit,
			priorBootstrapSHA, priorBundleSHA, priorPatchToolSHA,
			bootstrapNotesFile, bundleNotesFile, patchToolNotesFile,
			priorSecuritySHAs.values); err != nil {
			exitf("promote: %v", err)
		}
	}

	if pruneKeep >= 0 {
		pruned, err := releaseyaml.Prune(metadataPath, pruneKeep)
		if err != nil {
			exitf("prune: %v", err)
		}
		// Print pruned versions, one per line, on stdout. The release
		// workflow consumes this list to delete the matching artifact
		// directories on the www container. An empty list is a valid
		// outcome (no entries past keepN); print a header line so the
		// caller can distinguish "no-op" from "error".
		fmt.Fprintf(os.Stderr, "pruned %d external release entries (keep=%d)\n", len(pruned), pruneKeep)
		for _, v := range pruned {
			fmt.Println(v)
		}
	}

	meta, err := loadMetadata(metadataPath)
	if err != nil {
		exitf("load metadata: %v", err)
	}
	if err := validateMetadata(meta); err != nil {
		exitf("validate metadata: %v", err)
	}

	metadataDir := filepath.Dir(metadataPath)
	if !keepArtifacts {
		if err := os.RemoveAll(outputDir); err != nil {
			exitf("prepare output dir: %v", err)
		}
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		exitf("create output dir: %v", err)
	}

	rendered := renderedSite{
		Title:       defaultString(meta.Site.Title, "Aether Ops Bootstrap Downloads"),
		Description: defaultString(meta.Site.Description, "Download the current bootstrap launcher and offline bundle artifacts."),
		BaseURLPath: strings.TrimRight(defaultString(meta.Site.BaseURLPath, "/aether-ops-bootstrap"), "/"),
	}

	public := publicMetadata{
		SchemaVersion: meta.SchemaVersion,
		Site: siteConfig{
			Title:       rendered.Title,
			BaseURLPath: rendered.BaseURLPath,
			Description: rendered.Description,
		},
	}

	for _, rel := range meta.Releases {
		renderedRelease, publicRelease, err := materializeRelease(metadataDir, outputDir, rendered.BaseURLPath, rel, meta.Site.ArtifactKinds, keepArtifacts)
		if err != nil {
			exitf("materialize release %q: %v", rel.ID, err)
		}
		rendered.Releases = append(rendered.Releases, renderedRelease)
		public.Releases = append(public.Releases, publicRelease)
	}

	sort.SliceStable(rendered.Releases, func(i, j int) bool {
		if rendered.Releases[i].Current != rendered.Releases[j].Current {
			return rendered.Releases[i].Current
		}
		return rendered.Releases[i].PublishedAt > rendered.Releases[j].PublishedAt
	})
	sort.SliceStable(public.Releases, func(i, j int) bool {
		if public.Releases[i].Current != public.Releases[j].Current {
			return public.Releases[i].Current
		}
		return public.Releases[i].PublishedAt > public.Releases[j].PublishedAt
	})

	rendered.Latest = rendered.Releases[0]

	tmpl, err := parseTemplates()
	if err != nil {
		exitf("parse templates: %v", err)
	}

	if err := copyEmbeddedAssets(filepath.Join(outputDir, "assets")); err != nil {
		exitf("copy site assets: %v", err)
	}
	if err := writeTemplate(filepath.Join(outputDir, "index.html"), tmpl, "index.html.tmpl", rendered); err != nil {
		exitf("write index: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(outputDir, "releases"), 0o755); err != nil {
		exitf("create releases dir: %v", err)
	}
	if err := writeTemplate(filepath.Join(outputDir, "releases", "index.html"), tmpl, "releases.html.tmpl", rendered); err != nil {
		exitf("write releases index: %v", err)
	}
	if err := writeJSON(filepath.Join(outputDir, "metadata.json"), public); err != nil {
		exitf("write metadata json: %v", err)
	}
}

// parseTemplates loads every *.tmpl file under the embedded assets
// directory into a single template tree. Both the top-level pages
// (index.html.tmpl, releases.html.tmpl) and the shared artifact
// partials (_artifact_card.html.tmpl) share one FuncMap and one
// definitions namespace so {{ template "card" . }} resolves from
// either page.
func parseTemplates() (*template.Template, error) {
	funcs := template.FuncMap{
		"shortHash":       shortHash,
		"formatPublished": formatPublished,
	}
	return template.New("site").Funcs(funcs).ParseFS(assetsFS, "assets/*.tmpl")
}

// copyEmbeddedAssets writes every non-template static asset
// (currently only site.css) to <outputDir>/assets/. The page
// templates link to these via {{ .BaseURLPath }}/assets/site.css.
func copyEmbeddedAssets(dstDir string) error {
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return err
	}
	entries, err := fs.ReadDir(assetsFS, "assets")
	if err != nil {
		return err
	}
	for _, ent := range entries {
		if ent.IsDir() || strings.HasSuffix(ent.Name(), ".tmpl") {
			continue
		}
		body, err := fs.ReadFile(assetsFS, "assets/"+ent.Name())
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dstDir, ent.Name()), body, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func loadMetadata(path string) (siteMetadata, error) {
	var meta siteMetadata
	data, err := os.ReadFile(path)
	if err != nil {
		return meta, err
	}
	if err := yaml.Unmarshal(data, &meta); err != nil {
		return meta, err
	}
	return meta, nil
}

func validateMetadata(meta siteMetadata) error {
	if meta.SchemaVersion != 1 {
		return fmt.Errorf("unsupported schema_version %d", meta.SchemaVersion)
	}
	if len(meta.Releases) == 0 {
		return errors.New("at least one release is required")
	}
	currentCount := 0
	seenIDs := map[string]struct{}{}
	for _, rel := range meta.Releases {
		if rel.ID == "" {
			return errors.New("release id is required")
		}
		if _, ok := seenIDs[rel.ID]; ok {
			return fmt.Errorf("duplicate release id %q", rel.ID)
		}
		seenIDs[rel.ID] = struct{}{}
		if rel.Current {
			currentCount++
		}
		if err := validateArtifact("bootstrap", rel.Bootstrap, rel.External); err != nil {
			return fmt.Errorf("release %q: %w", rel.ID, err)
		}
		if err := validateArtifact("bundle", rel.Bundle, rel.External); err != nil {
			return fmt.Errorf("release %q: %w", rel.ID, err)
		}
		if rel.PatchTool != nil {
			if err := validateArtifact("patch_tool", *rel.PatchTool, rel.External); err != nil {
				return fmt.Errorf("release %q: %w", rel.ID, err)
			}
		}
	}
	if currentCount > 1 {
		return errors.New("only one release may be marked current")
	}
	return nil
}

func validateArtifact(kind string, artifact artifactConfig, external bool) error {
	if artifact.Version == "" {
		return fmt.Errorf("%s version is required", kind)
	}
	if artifact.Path == "" {
		return fmt.Errorf("%s path is required", kind)
	}
	if strings.Contains(artifact.Path, "/") || strings.Contains(artifact.Path, `\`) {
		return fmt.Errorf("%s path must be a single path segment", kind)
	}
	if artifact.Filename == "" {
		return fmt.Errorf("%s filename is required", kind)
	}
	seen := map[string]struct{}{}
	for i, sa := range artifact.SecurityArtifacts {
		if sa.Kind == "" {
			return fmt.Errorf("%s security_artifacts[%d]: kind is required", kind, i)
		}
		if sa.Filename == "" {
			return fmt.Errorf("%s security_artifacts[%d] (%s): filename is required", kind, i, sa.Kind)
		}
		if strings.ContainsAny(sa.Filename, "/\\") {
			return fmt.Errorf("%s security_artifacts[%d] (%s): filename must not contain a path separator", kind, i, sa.Kind)
		}
		if _, dup := seen[sa.Filename]; dup {
			return fmt.Errorf("%s security_artifacts: duplicate filename %q", kind, sa.Filename)
		}
		seen[sa.Filename] = struct{}{}
		if sa.Filename == artifact.Filename {
			return fmt.Errorf("%s security_artifacts[%d] (%s): filename collides with the primary artifact", kind, i, sa.Kind)
		}
		if external {
			if sa.SHA256 == "" {
				return fmt.Errorf("%s security_artifacts[%d] (%s): sha256 is required for external releases", kind, i, sa.Kind)
			}
			continue
		}
		if sa.Source == "" {
			return fmt.Errorf("%s security_artifacts[%d] (%s): source is required", kind, i, sa.Kind)
		}
	}
	if external {
		if artifact.SHA256 == "" {
			return fmt.Errorf("%s sha256 is required for external releases", kind)
		}
		return nil
	}
	if artifact.Source == "" {
		return fmt.Errorf("%s source is required", kind)
	}
	return nil
}

// artifactDir maps an artifact kind to its top-level directory inside
// the published site. The kind (bootstrap / bundle / patch_tool) is the
// stable identifier used in the YAML and metadata.json; the dir is the
// URL path segment.
var artifactDir = map[string]string{
	"bootstrap":  "bootstrap",
	"bundle":     "bundles",
	"patch_tool": "tools",
}

// defaultArtifactLabel is used when neither the per-release artifact
// entry nor the kinds map provides a label.
var defaultArtifactLabel = map[string]string{
	"bootstrap":  "Bootstrap Launcher",
	"bundle":     "Offline Bundle",
	"patch_tool": "Bundle Patch Tool",
}

func materializeRelease(metadataDir, outputDir, baseURL string, rel releaseConfig, kinds map[string]artifactKindDefaults, keepArtifacts bool) (renderedRelease, publicRelease, error) {
	bootstrapRA, bootstrapPA, err := materializeArtifact(metadataDir, outputDir, baseURL, "bootstrap", rel.Bootstrap, rel.External, kinds, keepArtifacts)
	if err != nil {
		return renderedRelease{}, publicRelease{}, fmt.Errorf("bootstrap: %w", err)
	}
	bundleRA, bundlePA, err := materializeArtifact(metadataDir, outputDir, baseURL, "bundle", rel.Bundle, rel.External, kinds, keepArtifacts)
	if err != nil {
		return renderedRelease{}, publicRelease{}, fmt.Errorf("bundle: %w", err)
	}

	renderedRel := renderedRelease{
		ID:          rel.ID,
		PublishedAt: rel.PublishedAt,
		Current:     rel.Current,
		External:    rel.External,
		Bootstrap:   bootstrapRA,
		Bundle:      bundleRA,
	}
	publicRel := publicRelease{
		ID:          rel.ID,
		PublishedAt: rel.PublishedAt,
		Current:     rel.Current,
		External:    rel.External,
		Bootstrap:   bootstrapPA,
		Bundle:      bundlePA,
	}

	if rel.PatchTool != nil {
		patchRA, patchPA, err := materializeArtifact(metadataDir, outputDir, baseURL, "patch_tool", *rel.PatchTool, rel.External, kinds, keepArtifacts)
		if err != nil {
			return renderedRelease{}, publicRelease{}, fmt.Errorf("patch_tool: %w", err)
		}
		renderedRel.PatchTool = &patchRA
		publicRel.PatchTool = &patchPA
	}

	return renderedRel, publicRel, nil
}

// materializeArtifact copies a single artifact into the output tree (or
// trusts a pre-existing SHA for external releases), composes URLs, and
// builds the rendered + public views. Description and DocsURL fall back
// to the matching kinds entry; per-release entries don't currently carry
// those fields directly, but adding them later is a one-line change.
//
// keepArtifacts switches the function to rerender-only mode for
// non-external releases: instead of copying art.Source into the output
// tree and hashing it, the SHA256 is read from the pre-existing
// <outputDir>/<dir>/<path>/<filename>.sha256 sidecar. The artifact
// file itself is left untouched.
func materializeArtifact(metadataDir, outputDir, baseURL, kind string, art artifactConfig, external bool, kinds map[string]artifactKindDefaults, keepArtifacts bool) (renderedArtifact, publicArtifact, error) {
	dir := artifactDir[kind]
	if dir == "" {
		return renderedArtifact{}, publicArtifact{}, fmt.Errorf("unknown artifact kind %q", kind)
	}
	defaults := kinds[kind]

	url := joinURL(baseURL, dir, art.Path, art.Filename)
	shaURL := joinURL(baseURL, dir, art.Path, art.Filename+".sha256")

	var hash string
	switch {
	case external:
		hash = art.SHA256
	case keepArtifacts:
		// Rerender-only path: read SHA from the existing sidecar that
		// the previous full publish wrote next to the artifact.
		sidecar := filepath.Join(outputDir, dir, art.Path, art.Filename+".sha256")
		h, err := parseSHA256File(sidecar)
		if err != nil {
			return renderedArtifact{}, publicArtifact{}, fmt.Errorf("--keep-existing-artifacts: reading %s: %w", sidecar, err)
		}
		hash = h
	default:
		stageDir := filepath.Join(outputDir, dir, art.Path)
		if err := os.MkdirAll(stageDir, 0o755); err != nil {
			return renderedArtifact{}, publicArtifact{}, err
		}
		var err error
		hash, err = copyArtifact(resolvePath(metadataDir, art.Source), filepath.Join(stageDir, art.Filename))
		if err != nil {
			return renderedArtifact{}, publicArtifact{}, err
		}
		if art.SHA256 != "" && art.SHA256 != hash {
			return renderedArtifact{}, publicArtifact{}, fmt.Errorf("sha256 mismatch: metadata=%s computed=%s", art.SHA256, hash)
		}
		if art.SHA256Source != "" {
			hashFromFile, err := parseSHA256File(resolvePath(metadataDir, art.SHA256Source))
			if err != nil {
				return renderedArtifact{}, publicArtifact{}, fmt.Errorf("sha256 source: %w", err)
			}
			if hashFromFile != hash {
				return renderedArtifact{}, publicArtifact{}, fmt.Errorf("sha256 source mismatch: file=%s computed=%s", hashFromFile, hash)
			}
		}
		if err := writeSHA256File(filepath.Join(stageDir, art.Filename+".sha256"), art.Filename, hash); err != nil {
			return renderedArtifact{}, publicArtifact{}, err
		}
	}

	renderedSecurity, publicSecurity, err := materializeSecurityArtifacts(metadataDir, outputDir, baseURL, kind, art, external, keepArtifacts)
	if err != nil {
		return renderedArtifact{}, publicArtifact{}, err
	}

	label := defaultString(defaultString(art.Label, defaults.Label), defaultArtifactLabel[kind])

	rendered := renderedArtifact{
		Kind:              kind,
		Label:             label,
		Description:       defaults.Description,
		DocsURL:           defaults.DocsURL,
		Version:           art.Version,
		Path:              art.Path,
		Filename:          art.Filename,
		SHA256:            hash,
		Commit:            art.Commit,
		BuildCommit:       art.BuildCommit,
		ReleaseSummary:    strings.TrimSpace(art.ReleaseSummary),
		ReleaseNotes:      defaultNotes(art.ReleaseNotes),
		URL:               url,
		SHA256URL:         shaURL,
		Components:        renderComponents(art.Components),
		SecurityArtifacts: renderedSecurity,
	}
	public := publicArtifact{
		Kind:              kind,
		Label:             rendered.Label,
		Description:       rendered.Description,
		DocsURL:           rendered.DocsURL,
		Version:           rendered.Version,
		Path:              rendered.Path,
		Filename:          rendered.Filename,
		SHA256:            rendered.SHA256,
		Commit:            rendered.Commit,
		BuildCommit:       rendered.BuildCommit,
		ReleaseSummary:    rendered.ReleaseSummary,
		ReleaseNotes:      rendered.ReleaseNotes,
		URL:               rendered.URL,
		SHA256URL:         rendered.SHA256URL,
		Components:        publicComponents(rendered.Components),
		SecurityArtifacts: publicSecurity,
	}
	return rendered, public, nil
}

// defaultSecurityArtifactLabel covers the well-known supply-chain kinds.
// Unknown kinds fall back to a Title-Case version of the kind string so
// new entries are renderable without a schema change.
//
// `grype` is the canonical Grype JSON; `grype-table` is the same scan
// rendered as a human-readable text table (no ANSI codes — Grype
// writes plain text when output is a file).
var defaultSecurityArtifactLabel = map[string]string{
	"sbom":         "SBOM (SPDX-JSON)",
	"grype":        "Grype scan (JSON)",
	"grype-table":  "Grype scan (table)",
	"image-sboms":  "Per-image SBOMs (tar.gz)",
	"image-grypes": "Per-image Grype scans (tar.gz)",
	"vex":          "OpenVEX statements",
}

// materializeSecurityArtifacts handles the SBOM / Grype / VEX sidecars
// attached to one artifact block. Each entry is staged under the same
// per-version directory as the primary artifact (so URLs stay tidy and
// nginx serves them with the same caching), hashed, and given a
// .sha256 sidecar. external releases trust the inlined SHA256 and skip
// the file copy; keepArtifacts mode reads each SHA from the existing
// sidecar without touching the artifact.
func materializeSecurityArtifacts(metadataDir, outputDir, baseURL, kind string, art artifactConfig, external, keepArtifacts bool) ([]renderedSecurityArtifact, []publicSecurityArtifact, error) {
	if len(art.SecurityArtifacts) == 0 {
		return nil, nil, nil
	}
	dir := artifactDir[kind]
	rendered := make([]renderedSecurityArtifact, 0, len(art.SecurityArtifacts))
	public := make([]publicSecurityArtifact, 0, len(art.SecurityArtifacts))
	for _, sa := range art.SecurityArtifacts {
		var hash string
		switch {
		case external:
			hash = sa.SHA256
		case keepArtifacts:
			sidecar := filepath.Join(outputDir, dir, art.Path, sa.Filename+".sha256")
			h, err := parseSHA256File(sidecar)
			if err != nil {
				return nil, nil, fmt.Errorf("%s security_artifacts (%s) --keep-existing-artifacts: reading %s: %w", kind, sa.Kind, sidecar, err)
			}
			hash = h
		default:
			stageDir := filepath.Join(outputDir, dir, art.Path)
			if err := os.MkdirAll(stageDir, 0o755); err != nil {
				return nil, nil, err
			}
			h, err := copyArtifact(resolvePath(metadataDir, sa.Source), filepath.Join(stageDir, sa.Filename))
			if err != nil {
				return nil, nil, fmt.Errorf("%s security_artifacts (%s): %w", kind, sa.Kind, err)
			}
			if sa.SHA256 != "" && sa.SHA256 != h {
				return nil, nil, fmt.Errorf("%s security_artifacts (%s): sha256 mismatch: metadata=%s computed=%s", kind, sa.Kind, sa.SHA256, h)
			}
			if err := writeSHA256File(filepath.Join(stageDir, sa.Filename+".sha256"), sa.Filename, h); err != nil {
				return nil, nil, err
			}
			hash = h
		}
		label := defaultString(sa.Label, defaultSecurityArtifactLabel[sa.Kind])
		if label == "" {
			label = strings.ToUpper(sa.Kind[:1]) + sa.Kind[1:]
		}
		url := joinURL(baseURL, dir, art.Path, sa.Filename)
		shaURL := joinURL(baseURL, dir, art.Path, sa.Filename+".sha256")
		rendered = append(rendered, renderedSecurityArtifact{
			Kind:      sa.Kind,
			Label:     label,
			Filename:  sa.Filename,
			SHA256:    hash,
			URL:       url,
			SHA256URL: shaURL,
		})
		public = append(public, publicSecurityArtifact{
			Kind:      sa.Kind,
			Label:     label,
			Filename:  sa.Filename,
			SHA256:    hash,
			URL:       url,
			SHA256URL: shaURL,
		})
	}
	return rendered, public, nil
}

func resolvePath(baseDir, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Clean(filepath.Join(baseDir, path))
}

func copyArtifact(src, dst string) (string, error) {
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	defer out.Close()

	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(out, hasher), in); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), out.Close()
}

func parseSHA256File(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return "", errors.New("empty sha256 file")
	}
	return fields[0], nil
}

func writeSHA256File(path, filename, hash string) error {
	return os.WriteFile(path, []byte(fmt.Sprintf("%s  %s\n", hash, filename)), 0o644)
}

func writeTemplate(path string, tmpl *template.Template, name string, data any) error {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func shortHash(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

// publishedTimeLayouts is the set of layouts the formatPublished helper
// will accept for a release's published_at field. The first match wins.
// Date-only strings are returned verbatim; layouts that include a clock
// component are normalized to "YYYY-MM-DD HH:MM" (seconds dropped) with
// a trailing UTC suffix when the input had a timezone.
var publishedTimeLayouts = []struct {
	layout  string
	hasZone bool
}{
	{time.RFC3339, true},
	{"2006-01-02T15:04:05", false},
	{"2006-01-02 15:04:05", false},
	{"2006-01-02 15:04", false},
	{"2006-01-02T15:04", false},
}

// formatPublished renders a release's published_at string for human
// display. Supports plain dates ("2026-04-29") verbatim plus several
// timestamp layouts; timestamps are shown as "YYYY-MM-DD HH:MM" with
// "UTC" appended when the input carried a timezone.
//
// Anything unparseable is returned unchanged so spec authors aren't
// punished for unusual formats — the cost is just a slightly less
// polished display.
func formatPublished(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	// Date only — present is preserved.
	if _, err := time.Parse("2006-01-02", s); err == nil {
		return s
	}
	for _, l := range publishedTimeLayouts {
		t, err := time.Parse(l.layout, s)
		if err != nil {
			continue
		}
		if l.hasZone {
			return t.UTC().Format("2006-01-02 15:04") + " UTC"
		}
		return t.Format("2006-01-02 15:04")
	}
	return raw
}

func renderComponents(in []componentConfig) []renderedComponent {
	if len(in) == 0 {
		return nil
	}
	out := make([]renderedComponent, len(in))
	for i, c := range in {
		out[i] = renderedComponent(c)
	}
	return out
}

func publicComponents(in []renderedComponent) []publicComponent {
	if len(in) == 0 {
		return nil
	}
	out := make([]publicComponent, len(in))
	for i, c := range in {
		out[i] = publicComponent(c)
	}
	return out
}

func writeJSON(path string, data any) error {
	buf, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	buf = append(buf, '\n')
	return os.WriteFile(path, buf, 0o644)
}

func joinURL(base string, parts ...string) string {
	all := []string{strings.TrimRight(base, "/")}
	all = append(all, parts...)
	return strings.Join(all, "/")
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

// defaultNotes returns the rendering-ready bullet list for one
// artifact's release notes. An empty list collapses to a single
// placeholder bullet so the rendered card always has a Release Notes
// section.
func defaultNotes(notes []string) []string {
	out := make([]string, 0, len(notes))
	for _, n := range notes {
		s := strings.TrimSpace(n)
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return []string{"No release notes provided."}
	}
	return out
}

// parseNotesFile splits the free-form text supplied via the
// --bootstrap-notes / --bundle-notes / --patch-tool-notes flag files
// into a one-line summary and the bullet list stored in releases.yaml.
//
// Authoring conventions:
//
//   - Any leading lines BEFORE the first "- " / "* " bullet line form
//     the summary. Multiple physical lines collapse into one paragraph
//     joined by spaces; blank lines inside the summary mark its end.
//   - Lines starting with "- " or "* " (after trimming whitespace)
//     begin a new bullet. Subsequent non-empty, non-prefix lines are
//     appended to the current bullet with a single space (so a bullet
//     can wrap across multiple physical lines in the source file).
//   - If no "- " / "* " prefix appears anywhere in the input, the first
//     paragraph (text up to the first blank line) becomes the summary
//     and remaining paragraphs each collapse into one bullet — useful
//     when migrating older paragraph-style notes without touching the
//     source format.
//
// Empty / whitespace-only input returns ("", nil).
func parseNotesFile(text string) (summary string, bullets []string) {
	if strings.TrimSpace(text) == "" {
		return "", nil
	}
	lines := strings.Split(text, "\n")

	hasBulletPrefix := false
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ") {
			hasBulletPrefix = true
			break
		}
	}

	var sum strings.Builder
	flushSummary := func() {
		summary = strings.TrimSpace(sum.String())
	}
	appendSummary := func(t string) {
		if sum.Len() > 0 {
			sum.WriteByte(' ')
		}
		sum.WriteString(t)
	}

	if hasBulletPrefix {
		var cur strings.Builder
		flush := func() {
			s := strings.TrimSpace(cur.String())
			if s != "" {
				bullets = append(bullets, s)
			}
			cur.Reset()
		}
		summaryDone := false
		for _, line := range lines {
			t := strings.TrimSpace(line)
			isBullet := strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ")
			if !summaryDone && !isBullet {
				if t == "" {
					if sum.Len() > 0 {
						summaryDone = true
					}
					continue
				}
				appendSummary(t)
				continue
			}
			summaryDone = true
			if isBullet {
				flush()
				cur.WriteString(strings.TrimSpace(t[2:]))
				continue
			}
			if t == "" {
				// Blank lines inside a bullet are ignored — bullets
				// are delimited by the next "- " marker.
				continue
			}
			if cur.Len() > 0 {
				cur.WriteByte(' ')
			}
			cur.WriteString(t)
		}
		flush()
		flushSummary()
		return summary, bullets
	}

	// Paragraph mode: first paragraph → summary, remaining paragraphs → bullets.
	var cur strings.Builder
	flushPara := func(first bool) {
		s := strings.TrimSpace(cur.String())
		cur.Reset()
		if s == "" {
			return
		}
		if first {
			summary = s
			return
		}
		bullets = append(bullets, s)
	}
	paraIdx := 0
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if t == "" {
			if cur.Len() > 0 {
				flushPara(paraIdx == 0)
				paraIdx++
			}
			continue
		}
		if cur.Len() > 0 {
			cur.WriteByte(' ')
		}
		cur.WriteString(t)
	}
	if cur.Len() > 0 {
		flushPara(paraIdx == 0)
	}
	return summary, bullets
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

// priorSecuritySHAFlag implements flag.Value to collect repeatable
// --prior-security-sha=<parent_kind>:<filename>=<hex> entries.
type priorSecuritySHAFlag struct {
	values []releaseyaml.PriorSecuritySHA
}

func (f *priorSecuritySHAFlag) String() string {
	parts := make([]string, 0, len(f.values))
	for _, v := range f.values {
		parts = append(parts, fmt.Sprintf("%s:%s=%s", v.ParentKind, v.Filename, v.SHA256))
	}
	return strings.Join(parts, ",")
}

func (f *priorSecuritySHAFlag) Set(raw string) error {
	eq := strings.IndexByte(raw, '=')
	if eq <= 0 || eq == len(raw)-1 {
		return fmt.Errorf("expected <parent_kind>:<filename>=<sha>, got %q", raw)
	}
	lhs, sha := raw[:eq], raw[eq+1:]
	colon := strings.IndexByte(lhs, ':')
	if colon <= 0 || colon == len(lhs)-1 {
		return fmt.Errorf("expected <parent_kind>:<filename> on the left of '=', got %q", lhs)
	}
	parent, filename := lhs[:colon], lhs[colon+1:]
	switch parent {
	case "bootstrap", "bundle", "patch_tool":
	default:
		return fmt.Errorf("parent_kind must be bootstrap, bundle, or patch_tool (got %q)", parent)
	}
	f.values = append(f.values, releaseyaml.PriorSecuritySHA{
		ParentKind: parent,
		Filename:   filename,
		SHA256:     sha,
	})
	return nil
}

// promoteRelease wraps internal/releaseyaml.Promote with the CLI's
// flag conventions: required flags are validated up front, release-
// notes files (when set) are read off disk verbatim, and the spec
// path is resolved against the metadata directory when relative.
func promoteRelease(metadataPath, specPath, version, id, buildCommit,
	priorBootstrapSHA, priorBundleSHA, priorPatchToolSHA,
	bootstrapNotesFile, bundleNotesFile, patchToolNotesFile string,
	priorSecurity []releaseyaml.PriorSecuritySHA) error {
	if buildCommit == "" {
		return errors.New("--build-commit is required with --promote-current")
	}
	if priorBootstrapSHA == "" || priorBundleSHA == "" || priorPatchToolSHA == "" {
		return errors.New("--prior-bootstrap-sha, --prior-bundle-sha, and --prior-patch-tool-sha are all required with --promote-current")
	}

	bootstrapSummary, bootstrapNotes, err := readNotesFile(bootstrapNotesFile)
	if err != nil {
		return fmt.Errorf("reading --bootstrap-notes: %w", err)
	}
	bundleSummary, bundleNotes, err := readNotesFile(bundleNotesFile)
	if err != nil {
		return fmt.Errorf("reading --bundle-notes: %w", err)
	}
	patchToolSummary, patchToolNotes, err := readNotesFile(patchToolNotesFile)
	if err != nil {
		return fmt.Errorf("reading --patch-tool-notes: %w", err)
	}

	// Resolve the spec path relative to the metadata file's directory
	// when it isn't already absolute. The metadata file lives at
	// site/releases.yaml and the spec at specs/bundle.yaml, so the
	// default relative path "specs/bundle.yaml" needs the repo root
	// in front of it. Use metadata's parent's parent as the repo
	// root anchor.
	if !filepath.IsAbs(specPath) {
		// Walk up from the metadata file to the repo root (its
		// grandparent in the common layout: <repo>/site/releases.yaml).
		// If the directory doesn't match expectations, fall back to
		// the CWD-relative spec path the caller passed in.
		if abs, err := filepath.Abs(filepath.Join(filepath.Dir(metadataPath), "..", specPath)); err == nil {
			if _, statErr := os.Stat(abs); statErr == nil {
				specPath = abs
			}
		}
	}

	return releaseyaml.Promote(releaseyaml.Options{
		YAMLPath:         metadataPath,
		SpecPath:         specPath,
		NewVersion:       version,
		ID:               id,
		BuildCommit:      buildCommit,
		BootstrapSummary: bootstrapSummary,
		BootstrapNotes:   bootstrapNotes,
		BundleSummary:    bundleSummary,
		BundleNotes:      bundleNotes,
		PatchToolSummary: patchToolSummary,
		PatchToolNotes:   patchToolNotes,
		Prior: releaseyaml.PriorSHAs{
			Bootstrap: priorBootstrapSHA,
			Bundle:    priorBundleSHA,
			PatchTool: priorPatchToolSHA,
		},
		PriorSecurity: priorSecurity,
	})
}

// readNotesFile reads a notes file off disk and returns the summary
// + bullet list that should be embedded into the new release entry.
// Empty path yields ("", nil, nil) so the caller can let the
// placeholder kick in.
func readNotesFile(path string) (string, []string, error) {
	if path == "" {
		return "", nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil, err
	}
	summary, bullets := parseNotesFile(string(data))
	return summary, bullets, nil
}
