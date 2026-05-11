package releaseyaml

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// minimalFixture builds a small but realistic releases.yaml with one
// current and one external release. The fixture mirrors the shape of
// the real site/releases.yaml so the test exercises every code path
// in demoteCurrent (source/sha256_source removal, sha256 inlining,
// external flag flip).
const minimalFixture = `schema_version: 1

site:
  title: Aether Ops Bootstrap Downloads
  base_url_path: /aether-ops-bootstrap
  description: Public download page.

releases:
  - id: "prior-current"
    published_at: "2026-05-07T21:45:00Z"
    current: true

    bootstrap:
      version: "2026.05.07.2"
      path: "2026.05.07.2"
      filename: aether-ops-bootstrap
      source: ../dist/aether-ops-bootstrap
      commit: "8669678"
      release_notes: |
        prior bootstrap notes.

    bundle:
      version: "2026.05.07.2"
      path: "2026.05.07.2"
      filename: bundle.tar.zst
      source: ../dist/bundle.tar.zst
      sha256_source: ../dist/bundle.tar.zst.sha256
      build_commit: "8669678"
      release_notes: |
        prior bundle notes.
      components:
        - name: aether-ops
          version: v0.1.50

    patch_tool:
      version: "2026.05.07.2"
      path: "2026.05.07.2"
      filename: patch-bundle
      source: ../dist/patch-bundle
      build_commit: "8669678"
      release_notes: |
        prior patch_tool notes.

  - id: "older-external"
    published_at: "2026-05-06T00:00:00Z"
    external: true

    bootstrap:
      version: "2026.05.06.1"
      path: "2026.05.06.1"
      filename: aether-ops-bootstrap
      sha256: "old-bootstrap-sha"
      commit: "abcdef1"

    bundle:
      version: "2026.05.06.1"
      path: "2026.05.06.1"
      filename: bundle.tar.zst
      sha256: "old-bundle-sha"
      build_commit: "abcdef1"

    patch_tool:
      version: "2026.05.06.1"
      path: "2026.05.06.1"
      filename: patch-bundle
      sha256: "old-patch-sha"
      build_commit: "abcdef1"
`

// minimalSpec is enough of specs/bundle.yaml for buildComponentsNode.
const minimalSpec = `schema_version: 2
bundle_version: "2026.05.11.1"

ubuntu:
  suites: [noble]
  architectures: [amd64]

rke2:
  version: "v1.35.3+rke2r3"
  variants: [canal]

helm:
  version: "v3.20.0"

aether_ops:
  version: "v0.2.0"

onramp:
  repo: https://github.com/opennetworkinglab/aether-onramp.git
  ref: 78f75c6a415c2e8567cd8c976a322e8be3d33da2

helm_charts:
  - name: sdcore-helm-charts
    repo: https://github.com/omec-project/sdcore-helm-charts.git
    ref: main

templates_dir: ./templates
`

func writeFixture(t *testing.T, dir, name, contents string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", path, err)
	}
	return path
}

func TestPromote_HappyPath(t *testing.T) {
	dir := t.TempDir()
	yamlPath := writeFixture(t, dir, "releases.yaml", minimalFixture)
	specPath := writeFixture(t, dir, "bundle.yaml", minimalSpec)

	publishedAt, _ := time.Parse(time.RFC3339, "2026-05-11T17:30:00Z")
	err := Promote(Options{
		YAMLPath:    yamlPath,
		SpecPath:    specPath,
		NewVersion:  "2026.05.11.1",
		ID:          "2026-05-11-aether-ops-v0.2.0",
		PublishedAt: publishedAt,
		BuildCommit: "d64127f",
		Prior: PriorSHAs{
			Bootstrap: "03613545bc33a02fce1f84a5c83b1a79f89e2185e314b5b6205177f7ef4a5209",
			Bundle:    "7d9a0212ed655fdef245685084988df9c87a08b8df04c907b10f8cbbd9b9a25f",
			PatchTool: "6ba45df841a7824673391b4ae21b70e070c27014788a7a3dd375a120e5c00368",
		},
		BootstrapNotes: "new bootstrap notes.\n",
		BundleNotes:    "new bundle notes.\n",
		PatchToolNotes: "new patch_tool notes.\n",
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	out, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)

	// New release entry must be at the top with current: true.
	for _, want := range []string{
		`id: "2026-05-11-aether-ops-v0.2.0"`,
		`published_at: "2026-05-11T17:30:00Z"`,
		`current: true`,
		`version: "2026.05.11.1"`,
		`source: ../dist/aether-ops-bootstrap`,
		`source: ../dist/bundle.tar.zst`,
		`sha256_source: ../dist/bundle.tar.zst.sha256`,
		`source: ../dist/patch-bundle`,
		`commit: "d64127f"`,
		`build_commit: "d64127f"`,
		`new bootstrap notes.`,
		`new bundle notes.`,
		`new patch_tool notes.`,
		`name: aether-ops`,
		`version: v0.2.0`,
		`name: aether-onramp`,
		`commit: 78f75c6a415c2e8567cd8c976a322e8be3d33da2`,
		`name: rke2`,
		`version: v1.35.3+rke2r3`,
		`name: helm`,
		`version: v3.20.0`,
		`name: sdcore-helm-charts`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\n--- got ---\n%s", want, got)
		}
	}

	// Old "prior-current" entry must now be external and carry the
	// inlined SHAs we provided.
	for _, want := range []string{
		`id: "prior-current"`,
		`external: true`,
		`sha256: "03613545bc33a02fce1f84a5c83b1a79f89e2185e314b5b6205177f7ef4a5209"`,
		`sha256: "7d9a0212ed655fdef245685084988df9c87a08b8df04c907b10f8cbbd9b9a25f"`,
		`sha256: "6ba45df841a7824673391b4ae21b70e070c27014788a7a3dd375a120e5c00368"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\n--- got ---\n%s", want, got)
		}
	}

	// The prior current entry's source: lines on bundle/bootstrap
	// must be gone — leaving them would silently overwrite published
	// artifacts on the next site build.
	for _, banned := range []string{
		`source: ../dist/aether-ops-bootstrap`,
	} {
		// source: lines are also on the NEW entry, so we can't just
		// substring-check; instead, verify the demoted entry no longer
		// has its old source pointer. Cheap way: assert there's at most
		// one occurrence (the new entry's).
		if strings.Count(got, banned) > 1 {
			t.Errorf("expected at most one occurrence of %q, found %d", banned, strings.Count(got, banned))
		}
	}

	// `current: true` should appear exactly once — on the new entry.
	if c := strings.Count(got, "current: true"); c != 1 {
		t.Errorf("expected exactly one `current: true`, got %d", c)
	}

	// External release that was already external must be left
	// untouched.
	if !strings.Contains(got, `id: "older-external"`) {
		t.Errorf("older-external entry was lost")
	}
}

func TestPromote_RejectsMissingCurrent(t *testing.T) {
	dir := t.TempDir()
	yamlPath := writeFixture(t, dir, "releases.yaml", strings.ReplaceAll(minimalFixture, "current: true", "external: true"))
	specPath := writeFixture(t, dir, "bundle.yaml", minimalSpec)

	err := Promote(Options{
		YAMLPath:    yamlPath,
		SpecPath:    specPath,
		NewVersion:  "2026.05.11.1",
		BuildCommit: "d64127f",
		Prior:       PriorSHAs{Bootstrap: "a", Bundle: "b", PatchTool: "c"},
	})
	if err == nil || !strings.Contains(err.Error(), "no release with `current: true`") {
		t.Fatalf("expected missing-current error, got %v", err)
	}
}

func TestPromote_RejectsMissingPriorSHA(t *testing.T) {
	dir := t.TempDir()
	yamlPath := writeFixture(t, dir, "releases.yaml", minimalFixture)
	specPath := writeFixture(t, dir, "bundle.yaml", minimalSpec)

	err := Promote(Options{
		YAMLPath:    yamlPath,
		SpecPath:    specPath,
		NewVersion:  "2026.05.11.1",
		BuildCommit: "d64127f",
		Prior:       PriorSHAs{Bootstrap: "only-bootstrap"},
	})
	if err == nil || !strings.Contains(err.Error(), "Prior SHAs are required") {
		t.Fatalf("expected missing-prior-sha error, got %v", err)
	}
}

func TestPromote_RejectsMissingBuildCommit(t *testing.T) {
	dir := t.TempDir()
	yamlPath := writeFixture(t, dir, "releases.yaml", minimalFixture)
	specPath := writeFixture(t, dir, "bundle.yaml", minimalSpec)

	err := Promote(Options{
		YAMLPath:   yamlPath,
		SpecPath:   specPath,
		NewVersion: "2026.05.11.1",
		Prior:      PriorSHAs{Bootstrap: "a", Bundle: "b", PatchTool: "c"},
	})
	if err == nil || !strings.Contains(err.Error(), "BuildCommit") {
		t.Fatalf("expected missing-build-commit error, got %v", err)
	}
}

func TestPromote_DefaultPublishedAt(t *testing.T) {
	dir := t.TempDir()
	yamlPath := writeFixture(t, dir, "releases.yaml", minimalFixture)
	specPath := writeFixture(t, dir, "bundle.yaml", minimalSpec)

	before := time.Now().UTC()
	err := Promote(Options{
		YAMLPath:    yamlPath,
		SpecPath:    specPath,
		NewVersion:  "2026.05.11.1",
		BuildCommit: "d64127f",
		Prior:       PriorSHAs{Bootstrap: "a", Bundle: "b", PatchTool: "c"},
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}
	out, _ := os.ReadFile(yamlPath)
	// Must contain a published_at within a sensible window of "now".
	// Match the date prefix to avoid flakiness on the seconds field.
	prefix := before.Format("2006-01-02")
	if !strings.Contains(string(out), `published_at: "`+prefix) {
		t.Errorf("expected published_at to default to today's UTC date %q; got:\n%s", prefix, out)
	}
}

func TestPromote_EmptyNotesUsesPlaceholder(t *testing.T) {
	dir := t.TempDir()
	yamlPath := writeFixture(t, dir, "releases.yaml", minimalFixture)
	specPath := writeFixture(t, dir, "bundle.yaml", minimalSpec)

	err := Promote(Options{
		YAMLPath:    yamlPath,
		SpecPath:    specPath,
		NewVersion:  "2026.05.11.1",
		BuildCommit: "d64127f",
		Prior:       PriorSHAs{Bootstrap: "a", Bundle: "b", PatchTool: "c"},
	})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}
	out, _ := os.ReadFile(yamlPath)
	if !strings.Contains(string(out), "TODO: fill in release notes before merging.") {
		t.Errorf("expected placeholder release_notes; got:\n%s", out)
	}
}
