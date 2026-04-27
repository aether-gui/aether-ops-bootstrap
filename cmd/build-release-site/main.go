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

	"gopkg.in/yaml.v3"
)

type siteMetadata struct {
	SchemaVersion int             `yaml:"schema_version"`
	Site          siteConfig      `yaml:"site"`
	Releases      []releaseConfig `yaml:"releases"`
}

type siteConfig struct {
	Title       string `yaml:"title" json:"title"`
	BaseURLPath string `yaml:"base_url_path" json:"base_url_path"`
	Description string `yaml:"description" json:"description"`
}

type releaseConfig struct {
	ID          string         `yaml:"id"`
	PublishedAt string         `yaml:"published_at"`
	Current     bool           `yaml:"current"`
	Bootstrap   artifactConfig `yaml:"bootstrap"`
	Bundle      artifactConfig `yaml:"bundle"`
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
	ReleaseNotes string `yaml:"release_notes"`
}

type publicMetadata struct {
	SchemaVersion int             `json:"schema_version"`
	Site          siteConfig      `json:"site"`
	Releases      []publicRelease `json:"releases"`
}

type publicRelease struct {
	ID          string         `json:"id"`
	PublishedAt string         `json:"published_at"`
	Current     bool           `json:"current"`
	Bootstrap   publicArtifact `json:"bootstrap"`
	Bundle      publicArtifact `json:"bundle"`
}

type publicArtifact struct {
	Label        string `json:"label"`
	Version      string `json:"version"`
	Path         string `json:"path"`
	Filename     string `json:"filename"`
	SHA256       string `json:"sha256"`
	Commit       string `json:"commit,omitempty"`
	BuildCommit  string `json:"build_commit,omitempty"`
	ReleaseNotes string `json:"release_notes,omitempty"`
	URL          string `json:"url"`
	SHA256URL    string `json:"sha256_url,omitempty"`
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
	Bootstrap   renderedArtifact
	Bundle      renderedArtifact
}

type renderedArtifact struct {
	Label        string
	Version      string
	Path         string
	Filename     string
	SHA256       string
	Commit       string
	BuildCommit  string
	ReleaseNotes string
	URL          string
	SHA256URL    string
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
      grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
      margin-top: 28px;
    }
    .card {
      background: var(--panel);
      border: 1px solid var(--border);
      border-radius: 16px;
      padding: 20px;
      box-shadow: 0 10px 30px rgba(16, 32, 51, 0.06);
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
    .footer-link {
      margin-top: 32px;
    }
  </style>
</head>
<body>
  <main>
    <h1>{{ .Title }}</h1>
    <p class="lede">{{ .Description }}</p>
    <p class="meta">Latest published release: {{ .Latest.PublishedAt }}</p>

    <section class="grid">
      <article class="card">
        <h2>{{ .Latest.Bootstrap.Label }}</h2>
        <p class="meta">Version {{ .Latest.Bootstrap.Version }}{{ if .Latest.Bootstrap.Commit }} · commit {{ .Latest.Bootstrap.Commit }}{{ end }}</p>
        <div class="linklist">
          <a href="{{ .Latest.Bootstrap.URL }}">Download {{ .Latest.Bootstrap.Filename }}</a>
        </div>
        <h3>SHA256</h3>
        <code>{{ .Latest.Bootstrap.SHA256 }}</code>
        <h3>Release Notes</h3>
        <div class="notes">{{ .Latest.Bootstrap.ReleaseNotes }}</div>
      </article>

      <article class="card">
        <h2>{{ .Latest.Bundle.Label }}</h2>
        <p class="meta">Version {{ .Latest.Bundle.Version }}{{ if .Latest.Bundle.BuildCommit }} · build {{ .Latest.Bundle.BuildCommit }}{{ end }}</p>
        <div class="linklist">
          <a href="{{ .Latest.Bundle.URL }}">Download {{ .Latest.Bundle.Filename }}</a>
          <a href="{{ .Latest.Bundle.SHA256URL }}">Download SHA256</a>
        </div>
        <h3>SHA256</h3>
        <code>{{ .Latest.Bundle.SHA256 }}</code>
        <h3>Release Notes</h3>
        <div class="notes">{{ .Latest.Bundle.ReleaseNotes }}</div>
      </article>
    </section>

    <p class="footer-link"><a href="{{ .BaseURLPath }}/releases/">See older versions</a></p>
  </main>
</body>
</html>
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
      grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
    }
    .artifact {
      background: #fbfcfe;
      border: 1px solid var(--border);
      border-radius: 12px;
      padding: 16px;
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
  </style>
</head>
<body>
  <main>
    <p><a href="{{ .BaseURLPath }}/">Back to latest release</a></p>
    <h1>{{ .Title }} Release History</h1>
    <p>{{ .Description }}</p>
    {{ range .Releases }}
    <section class="release">
      <h2>{{ .PublishedAt }}{{ if .Current }} · current{{ end }}</h2>
      <div class="artifacts">
        <article class="artifact">
          <h3>{{ .Bootstrap.Label }}</h3>
          <p class="meta">Version {{ .Bootstrap.Version }}{{ if .Bootstrap.Commit }} · commit {{ .Bootstrap.Commit }}{{ end }}</p>
          <div class="linklist">
            <a href="{{ .Bootstrap.URL }}">Download {{ .Bootstrap.Filename }}</a>
          </div>
          <code>{{ .Bootstrap.SHA256 }}</code>
          <div class="notes">{{ .Bootstrap.ReleaseNotes }}</div>
        </article>
        <article class="artifact">
          <h3>{{ .Bundle.Label }}</h3>
          <p class="meta">Version {{ .Bundle.Version }}{{ if .Bundle.BuildCommit }} · build {{ .Bundle.BuildCommit }}{{ end }}</p>
          <div class="linklist">
            <a href="{{ .Bundle.URL }}">Download {{ .Bundle.Filename }}</a>
            <a href="{{ .Bundle.SHA256URL }}">Download SHA256</a>
          </div>
          <code>{{ .Bundle.SHA256 }}</code>
          <div class="notes">{{ .Bundle.ReleaseNotes }}</div>
        </article>
      </div>
    </section>
    {{ end }}
  </main>
</body>
</html>
`

func main() {
	var metadataPath string
	var outputDir string

	flag.StringVar(&metadataPath, "metadata", "", "path to release metadata YAML")
	flag.StringVar(&outputDir, "output", "", "output directory for generated site")
	flag.Parse()

	if metadataPath == "" || outputDir == "" {
		flag.Usage()
		os.Exit(2)
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
		renderedRelease, publicRelease, err := materializeRelease(metadataDir, outputDir, rendered.BaseURLPath, rel)
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
		if err := validateArtifact("bootstrap", rel.Bootstrap); err != nil {
			return fmt.Errorf("release %q: %w", rel.ID, err)
		}
		if err := validateArtifact("bundle", rel.Bundle); err != nil {
			return fmt.Errorf("release %q: %w", rel.ID, err)
		}
	}
	if currentCount > 1 {
		return errors.New("only one release may be marked current")
	}
	return nil
}

func validateArtifact(kind string, artifact artifactConfig) error {
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
	if artifact.Source == "" {
		return fmt.Errorf("%s source is required", kind)
	}
	return nil
}

func materializeRelease(metadataDir, outputDir, baseURL string, rel releaseConfig) (renderedRelease, publicRelease, error) {
	bootstrapDir := filepath.Join(outputDir, "bootstrap", rel.Bootstrap.Path)
	bundleDir := filepath.Join(outputDir, "bundles", rel.Bundle.Path)
	if err := os.MkdirAll(bootstrapDir, 0o755); err != nil {
		return renderedRelease{}, publicRelease{}, err
	}
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		return renderedRelease{}, publicRelease{}, err
	}

	bootstrapURL := joinURL(baseURL, "bootstrap", rel.Bootstrap.Path, rel.Bootstrap.Filename)
	bundleURL := joinURL(baseURL, "bundles", rel.Bundle.Path, rel.Bundle.Filename)
	bundleSHAURL := joinURL(baseURL, "bundles", rel.Bundle.Path, rel.Bundle.Filename+".sha256")
	bootstrapSHAURL := joinURL(baseURL, "bootstrap", rel.Bootstrap.Path, rel.Bootstrap.Filename+".sha256")

	bootstrapHash, err := copyArtifact(resolvePath(metadataDir, rel.Bootstrap.Source), filepath.Join(bootstrapDir, rel.Bootstrap.Filename))
	if err != nil {
		return renderedRelease{}, publicRelease{}, fmt.Errorf("bootstrap: %w", err)
	}
	if rel.Bootstrap.SHA256 != "" && rel.Bootstrap.SHA256 != bootstrapHash {
		return renderedRelease{}, publicRelease{}, fmt.Errorf("bootstrap sha256 mismatch: metadata=%s computed=%s", rel.Bootstrap.SHA256, bootstrapHash)
	}
	if err := writeSHA256File(filepath.Join(bootstrapDir, rel.Bootstrap.Filename+".sha256"), rel.Bootstrap.Filename, bootstrapHash); err != nil {
		return renderedRelease{}, publicRelease{}, err
	}

	bundleHash, err := copyArtifact(resolvePath(metadataDir, rel.Bundle.Source), filepath.Join(bundleDir, rel.Bundle.Filename))
	if err != nil {
		return renderedRelease{}, publicRelease{}, fmt.Errorf("bundle: %w", err)
	}
	if rel.Bundle.SHA256 != "" && rel.Bundle.SHA256 != bundleHash {
		return renderedRelease{}, publicRelease{}, fmt.Errorf("bundle sha256 mismatch: metadata=%s computed=%s", rel.Bundle.SHA256, bundleHash)
	}
	if rel.Bundle.SHA256Source != "" {
		hashFromFile, err := parseSHA256File(resolvePath(metadataDir, rel.Bundle.SHA256Source))
		if err != nil {
			return renderedRelease{}, publicRelease{}, fmt.Errorf("bundle sha256 source: %w", err)
		}
		if hashFromFile != bundleHash {
			return renderedRelease{}, publicRelease{}, fmt.Errorf("bundle sha256 source mismatch: file=%s computed=%s", hashFromFile, bundleHash)
		}
	}
	if err := writeSHA256File(filepath.Join(bundleDir, rel.Bundle.Filename+".sha256"), rel.Bundle.Filename, bundleHash); err != nil {
		return renderedRelease{}, publicRelease{}, err
	}

	renderedRel := renderedRelease{
		ID:          rel.ID,
		PublishedAt: rel.PublishedAt,
		Current:     rel.Current,
		Bootstrap: renderedArtifact{
			Label:        defaultString(rel.Bootstrap.Label, "Bootstrap Launcher"),
			Version:      rel.Bootstrap.Version,
			Path:         rel.Bootstrap.Path,
			Filename:     rel.Bootstrap.Filename,
			SHA256:       bootstrapHash,
			Commit:       rel.Bootstrap.Commit,
			ReleaseNotes: defaultNotes(rel.Bootstrap.ReleaseNotes),
			URL:          bootstrapURL,
			SHA256URL:    bootstrapSHAURL,
		},
		Bundle: renderedArtifact{
			Label:        defaultString(rel.Bundle.Label, "Offline Bundle"),
			Version:      rel.Bundle.Version,
			Path:         rel.Bundle.Path,
			Filename:     rel.Bundle.Filename,
			SHA256:       bundleHash,
			BuildCommit:  rel.Bundle.BuildCommit,
			ReleaseNotes: defaultNotes(rel.Bundle.ReleaseNotes),
			URL:          bundleURL,
			SHA256URL:    bundleSHAURL,
		},
	}

	publicRel := publicRelease{
		ID:          rel.ID,
		PublishedAt: rel.PublishedAt,
		Current:     rel.Current,
		Bootstrap: publicArtifact{
			Label:        renderedRel.Bootstrap.Label,
			Version:      renderedRel.Bootstrap.Version,
			Path:         renderedRel.Bootstrap.Path,
			Filename:     renderedRel.Bootstrap.Filename,
			SHA256:       renderedRel.Bootstrap.SHA256,
			Commit:       renderedRel.Bootstrap.Commit,
			ReleaseNotes: renderedRel.Bootstrap.ReleaseNotes,
			URL:          renderedRel.Bootstrap.URL,
			SHA256URL:    renderedRel.Bootstrap.SHA256URL,
		},
		Bundle: publicArtifact{
			Label:        renderedRel.Bundle.Label,
			Version:      renderedRel.Bundle.Version,
			Path:         renderedRel.Bundle.Path,
			Filename:     renderedRel.Bundle.Filename,
			SHA256:       renderedRel.Bundle.SHA256,
			BuildCommit:  renderedRel.Bundle.BuildCommit,
			ReleaseNotes: renderedRel.Bundle.ReleaseNotes,
			URL:          renderedRel.Bundle.URL,
			SHA256URL:    renderedRel.Bundle.SHA256URL,
		},
	}

	return renderedRel, publicRel, nil
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
	t, err := template.New(filepath.Base(path)).Parse(tmpl)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
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
