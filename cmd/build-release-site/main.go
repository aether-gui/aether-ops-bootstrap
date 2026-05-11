package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aether-gui/aether-ops-bootstrap/internal/releaseyaml"
	"gopkg.in/yaml.v3"
)

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
	Label        string            `yaml:"label"`
	Version      string            `yaml:"version"`
	Path         string            `yaml:"path"`
	Filename     string            `yaml:"filename"`
	Source       string            `yaml:"source"`
	SHA256       string            `yaml:"sha256"`
	SHA256Source string            `yaml:"sha256_source"`
	Commit       string            `yaml:"commit"`
	BuildCommit  string            `yaml:"build_commit"`
	ReleaseNotes string            `yaml:"release_notes"`
	Components   []componentConfig `yaml:"components"`
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
	Kind         string            `json:"kind"` // "bootstrap" | "bundle" | "patch_tool"
	Label        string            `json:"label"`
	Description  string            `json:"description,omitempty"`
	DocsURL      string            `json:"docs_url,omitempty"`
	Version      string            `json:"version"`
	Path         string            `json:"path"`
	Filename     string            `json:"filename"`
	SHA256       string            `json:"sha256"`
	Commit       string            `json:"commit,omitempty"`
	BuildCommit  string            `json:"build_commit,omitempty"`
	ReleaseNotes string            `json:"release_notes,omitempty"`
	URL          string            `json:"url"`
	SHA256URL    string            `json:"sha256_url,omitempty"`
	Components   []publicComponent `json:"components,omitempty"`
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
	Kind         string // "bootstrap" | "bundle" | "patch_tool"
	Label        string
	Description  string
	DocsURL      string
	Version      string
	Path         string
	Filename     string
	SHA256       string
	Commit       string
	BuildCommit  string
	ReleaseNotes string
	URL          string
	SHA256URL    string
	Components   []renderedComponent
}

type renderedComponent struct {
	Name    string
	Version string
	Commit  string
}

const indexTemplate = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{ .Title }}</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #f6f8fb;
      --panel: #ffffff;
      --border: #d8e0ea;
      --text: #102033;
      --muted: #526173;
      --link: #0a5bd8;
      --code: #eef3f8;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      font-family: "IBM Plex Sans", "Segoe UI", sans-serif;
      background: linear-gradient(180deg, #eef4fb 0%, #f9fbfd 100%);
      color: var(--text);
    }
    main {
      max-width: 960px;
      margin: 0 auto;
      padding: 48px 20px 64px;
    }
    h1, h2, h3 { margin: 0 0 12px; }
    p { line-height: 1.5; }
    .lede { color: var(--muted); max-width: 70ch; }
    .grid {
      display: grid;
      gap: 20px;
      grid-template-columns: 1fr;
      margin-top: 28px;
    }
    .card {
      background: var(--panel);
      border: 1px solid var(--border);
      border-radius: 16px;
      padding: 20px;
      box-shadow: 0 10px 30px rgba(16, 32, 51, 0.06);
    }
    .blurb {
      color: var(--text);
      font-size: 0.95rem;
      margin: 0 0 14px;
    }
    .meta {
      color: var(--muted);
      font-size: 0.95rem;
      margin-bottom: 14px;
    }
    .linklist a {
      display: inline-block;
      margin-right: 14px;
      margin-bottom: 10px;
      color: var(--link);
      text-decoration: none;
      font-weight: 600;
    }
    code {
      display: block;
      overflow-wrap: anywhere;
      background: var(--code);
      border-radius: 10px;
      padding: 12px;
      font-family: "IBM Plex Mono", "SFMono-Regular", monospace;
      font-size: 0.9rem;
    }
    .notes {
      white-space: pre-wrap;
      background: #fbfcfe;
      border-left: 4px solid #b8c8dd;
      padding: 12px 14px;
      border-radius: 8px;
    }
    .components {
      margin: 0;
      padding: 0;
      list-style: none;
      border-top: 1px solid var(--border);
    }
    .components li {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
      align-items: baseline;
      padding: 8px 0;
      border-bottom: 1px solid var(--border);
      font-size: 0.9rem;
    }
    .components .name {
      font-weight: 600;
      flex: 0 0 auto;
    }
    .components .ver {
      color: var(--muted);
      flex: 0 0 auto;
    }
    .components .hash {
      font-family: "IBM Plex Mono", "SFMono-Regular", monospace;
      background: var(--code);
      padding: 1px 6px;
      border-radius: 6px;
      font-size: 0.85rem;
      flex: 0 0 auto;
    }
    .footer-link {
      margin-top: 32px;
    }
  </style>
</head>
<body>
  <main>
    <h1>{{ .Title }}</h1>
    <p class="lede">{{ .Description }}</p>
    <p class="meta">Latest published release: {{ formatPublished .Latest.PublishedAt }}</p>

    <section class="grid">
      {{ template "card" .Latest.Bootstrap }}
      {{ if .Latest.PatchTool }}{{ template "card" .Latest.PatchTool }}{{ end }}
      {{ template "card" .Latest.Bundle }}
    </section>

    <p class="footer-link"><a href="{{ .BaseURLPath }}/releases/">See older versions</a></p>
  </main>
</body>
</html>
{{ define "card" }}
<article class="card">
  <h2>{{ .Label }}</h2>
  {{ if .Description }}<p class="blurb">{{ .Description }}</p>{{ end }}
  <p class="meta">Version {{ .Version }}{{ if .Commit }} · commit <span class="hash">{{ shortHash .Commit }}</span>{{ else if .BuildCommit }} · build <span class="hash">{{ shortHash .BuildCommit }}</span>{{ end }}</p>
  <div class="linklist">
    <a href="{{ .URL }}">Download {{ .Filename }}</a>
    {{ if .SHA256URL }}<a href="{{ .SHA256URL }}">Download SHA256</a>{{ end }}
    {{ if .DocsURL }}<a href="{{ .DocsURL }}" rel="noopener" target="_blank">Documentation</a>{{ end }}
  </div>
  <h3>SHA256</h3>
  <code>{{ .SHA256 }}</code>
  <h3>Release Notes</h3>
  <div class="notes">{{ .ReleaseNotes }}</div>
  {{ if .Components }}
  <h3>Components</h3>
  <ul class="components">
    {{ range .Components }}
    <li>
      <span class="name">{{ .Name }}</span>
      {{ if .Version }}<span class="ver">{{ .Version }}</span>{{ end }}
      {{ if .Commit }}<span class="hash">{{ shortHash .Commit }}</span>{{ end }}
    </li>
    {{ end }}
  </ul>
  {{ end }}
</article>
{{ end }}
`

const releasesTemplate = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{ .Title }} - Releases</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #f6f8fb;
      --panel: #ffffff;
      --border: #d8e0ea;
      --text: #102033;
      --muted: #526173;
      --link: #0a5bd8;
      --code: #eef3f8;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      font-family: "IBM Plex Sans", "Segoe UI", sans-serif;
      background: linear-gradient(180deg, #eef4fb 0%, #f9fbfd 100%);
      color: var(--text);
    }
    main {
      max-width: 1040px;
      margin: 0 auto;
      padding: 48px 20px 64px;
    }
    .release {
      background: var(--panel);
      border: 1px solid var(--border);
      border-radius: 16px;
      padding: 20px;
      margin-top: 20px;
      box-shadow: 0 10px 30px rgba(16, 32, 51, 0.06);
    }
    .artifacts {
      display: grid;
      gap: 18px;
      grid-template-columns: 1fr;
    }
    .artifact {
      background: #fbfcfe;
      border: 1px solid var(--border);
      border-radius: 12px;
      padding: 16px;
    }
    .blurb {
      color: var(--text);
      font-size: 0.9rem;
      margin: 0 0 10px;
    }
    .meta {
      color: var(--muted);
      font-size: 0.95rem;
      margin-bottom: 10px;
    }
    .linklist a {
      display: inline-block;
      margin-right: 14px;
      margin-bottom: 10px;
      color: var(--link);
      text-decoration: none;
      font-weight: 600;
    }
    code {
      display: block;
      overflow-wrap: anywhere;
      background: var(--code);
      border-radius: 10px;
      padding: 12px;
      font-family: "IBM Plex Mono", "SFMono-Regular", monospace;
      font-size: 0.85rem;
    }
    .notes {
      white-space: pre-wrap;
      margin-top: 10px;
    }
    .components {
      margin: 10px 0 0;
      padding: 0;
      list-style: none;
      border-top: 1px solid var(--border);
    }
    .components li {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
      align-items: baseline;
      padding: 6px 0;
      border-bottom: 1px solid var(--border);
      font-size: 0.85rem;
    }
    .components .name { font-weight: 600; }
    .components .ver { color: var(--muted); }
    .components .hash {
      font-family: "IBM Plex Mono", "SFMono-Regular", monospace;
      background: var(--code);
      padding: 1px 6px;
      border-radius: 6px;
      font-size: 0.8rem;
    }
  </style>
</head>
<body>
  <main>
    <p><a href="{{ .BaseURLPath }}/">Back to latest release</a></p>
    <h1>{{ .Title }} Release History</h1>
    <p>{{ .Description }}</p>
    {{ range .Releases }}
    <section class="release">
      <h2>{{ formatPublished .PublishedAt }}{{ if .Current }} · current{{ end }}</h2>
      <div class="artifacts">
        {{ template "release-artifact" .Bootstrap }}
        {{ if .PatchTool }}{{ template "release-artifact" .PatchTool }}{{ end }}
        {{ template "release-artifact" .Bundle }}
      </div>
    </section>
    {{ end }}
  </main>
</body>
</html>
{{ define "release-artifact" }}
<article class="artifact">
  <h3>{{ .Label }}</h3>
  {{ if .Description }}<p class="blurb">{{ .Description }}</p>{{ end }}
  <p class="meta">Version {{ .Version }}{{ if .Commit }} · commit <span class="hash">{{ shortHash .Commit }}</span>{{ else if .BuildCommit }} · build <span class="hash">{{ shortHash .BuildCommit }}</span>{{ end }}</p>
  <div class="linklist">
    <a href="{{ .URL }}">Download {{ .Filename }}</a>
    {{ if .SHA256URL }}<a href="{{ .SHA256URL }}">Download SHA256</a>{{ end }}
    {{ if .DocsURL }}<a href="{{ .DocsURL }}" rel="noopener" target="_blank">Documentation</a>{{ end }}
  </div>
  <code>{{ .SHA256 }}</code>
  <div class="notes">{{ .ReleaseNotes }}</div>
  {{ if .Components }}
  <ul class="components">
    {{ range .Components }}
    <li>
      <span class="name">{{ .Name }}</span>
      {{ if .Version }}<span class="ver">{{ .Version }}</span>{{ end }}
      {{ if .Commit }}<span class="hash">{{ shortHash .Commit }}</span>{{ end }}
    </li>
    {{ end }}
  </ul>
  {{ end }}
</article>
{{ end }}
`

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
	flag.Parse()

	if metadataPath == "" || outputDir == "" {
		flag.Usage()
		os.Exit(2)
	}

	if promoteVersion != "" {
		if err := promoteRelease(metadataPath, promoteSpec, promoteVersion, promoteID, promoteCommit,
			priorBootstrapSHA, priorBundleSHA, priorPatchToolSHA,
			bootstrapNotesFile, bundleNotesFile, patchToolNotesFile); err != nil {
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
	if err := os.RemoveAll(outputDir); err != nil {
		exitf("prepare output dir: %v", err)
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
		renderedRelease, publicRelease, err := materializeRelease(metadataDir, outputDir, rendered.BaseURLPath, rel, meta.Site.ArtifactKinds)
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

	if err := writeTemplate(filepath.Join(outputDir, "index.html"), indexTemplate, rendered); err != nil {
		exitf("write index: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(outputDir, "releases"), 0o755); err != nil {
		exitf("create releases dir: %v", err)
	}
	if err := writeTemplate(filepath.Join(outputDir, "releases", "index.html"), releasesTemplate, rendered); err != nil {
		exitf("write releases index: %v", err)
	}
	if err := writeJSON(filepath.Join(outputDir, "metadata.json"), public); err != nil {
		exitf("write metadata json: %v", err)
	}
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

func materializeRelease(metadataDir, outputDir, baseURL string, rel releaseConfig, kinds map[string]artifactKindDefaults) (renderedRelease, publicRelease, error) {
	bootstrapRA, bootstrapPA, err := materializeArtifact(metadataDir, outputDir, baseURL, "bootstrap", rel.Bootstrap, rel.External, kinds)
	if err != nil {
		return renderedRelease{}, publicRelease{}, fmt.Errorf("bootstrap: %w", err)
	}
	bundleRA, bundlePA, err := materializeArtifact(metadataDir, outputDir, baseURL, "bundle", rel.Bundle, rel.External, kinds)
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
		patchRA, patchPA, err := materializeArtifact(metadataDir, outputDir, baseURL, "patch_tool", *rel.PatchTool, rel.External, kinds)
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
func materializeArtifact(metadataDir, outputDir, baseURL, kind string, art artifactConfig, external bool, kinds map[string]artifactKindDefaults) (renderedArtifact, publicArtifact, error) {
	dir := artifactDir[kind]
	if dir == "" {
		return renderedArtifact{}, publicArtifact{}, fmt.Errorf("unknown artifact kind %q", kind)
	}
	defaults := kinds[kind]

	url := joinURL(baseURL, dir, art.Path, art.Filename)
	shaURL := joinURL(baseURL, dir, art.Path, art.Filename+".sha256")

	var hash string
	if external {
		hash = art.SHA256
	} else {
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

	label := defaultString(defaultString(art.Label, defaults.Label), defaultArtifactLabel[kind])

	rendered := renderedArtifact{
		Kind:         kind,
		Label:        label,
		Description:  defaults.Description,
		DocsURL:      defaults.DocsURL,
		Version:      art.Version,
		Path:         art.Path,
		Filename:     art.Filename,
		SHA256:       hash,
		Commit:       art.Commit,
		BuildCommit:  art.BuildCommit,
		ReleaseNotes: defaultNotes(art.ReleaseNotes),
		URL:          url,
		SHA256URL:    shaURL,
		Components:   renderComponents(art.Components),
	}
	public := publicArtifact{
		Kind:         kind,
		Label:        rendered.Label,
		Description:  rendered.Description,
		DocsURL:      rendered.DocsURL,
		Version:      rendered.Version,
		Path:         rendered.Path,
		Filename:     rendered.Filename,
		SHA256:       rendered.SHA256,
		Commit:       rendered.Commit,
		BuildCommit:  rendered.BuildCommit,
		ReleaseNotes: rendered.ReleaseNotes,
		URL:          rendered.URL,
		SHA256URL:    rendered.SHA256URL,
		Components:   publicComponents(rendered.Components),
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

func writeTemplate(path, tmpl string, data any) error {
	funcs := template.FuncMap{
		"shortHash":       shortHash,
		"formatPublished": formatPublished,
	}
	t, err := template.New(filepath.Base(path)).Funcs(funcs).Parse(tmpl)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
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

func defaultNotes(notes string) string {
	trimmed := strings.TrimSpace(notes)
	if trimmed == "" {
		return "No release notes provided."
	}
	return trimmed
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

// promoteRelease wraps internal/releaseyaml.Promote with the CLI's
// flag conventions: required flags are validated up front, release-
// notes files (when set) are read off disk verbatim, and the spec
// path is resolved against the metadata directory when relative.
func promoteRelease(metadataPath, specPath, version, id, buildCommit,
	priorBootstrapSHA, priorBundleSHA, priorPatchToolSHA,
	bootstrapNotesFile, bundleNotesFile, patchToolNotesFile string) error {
	if buildCommit == "" {
		return errors.New("--build-commit is required with --promote-current")
	}
	if priorBootstrapSHA == "" || priorBundleSHA == "" || priorPatchToolSHA == "" {
		return errors.New("--prior-bootstrap-sha, --prior-bundle-sha, and --prior-patch-tool-sha are all required with --promote-current")
	}

	bootstrapNotes, err := readNotesFile(bootstrapNotesFile)
	if err != nil {
		return fmt.Errorf("reading --bootstrap-notes: %w", err)
	}
	bundleNotes, err := readNotesFile(bundleNotesFile)
	if err != nil {
		return fmt.Errorf("reading --bundle-notes: %w", err)
	}
	patchToolNotes, err := readNotesFile(patchToolNotesFile)
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
		YAMLPath:       metadataPath,
		SpecPath:       specPath,
		NewVersion:     version,
		ID:             id,
		BuildCommit:    buildCommit,
		BootstrapNotes: bootstrapNotes,
		BundleNotes:    bundleNotes,
		PatchToolNotes: patchToolNotes,
		Prior: releaseyaml.PriorSHAs{
			Bootstrap: priorBootstrapSHA,
			Bundle:    priorBundleSHA,
			PatchTool: priorPatchToolSHA,
		},
	})
}

func readNotesFile(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	body := string(data)
	// Ensure the literal block ends with exactly one newline so the
	// yaml encoder doesn't add a trailing chomp indicator.
	body = strings.TrimRight(body, "\n") + "\n"
	return body, nil
}
