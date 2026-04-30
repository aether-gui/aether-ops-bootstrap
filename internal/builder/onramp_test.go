package builder

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
)

// setupGitFixture creates a local bare-ish git repo the builder can clone
// over the `file://` transport. It returns a URL suitable for passing to
// git clone and the commit SHA of HEAD.
func setupGitFixture(t *testing.T, files map[string]string) (url, sha string) {
	t.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	repoDir := t.TempDir()
	runGit(t, repoDir, "init", "--initial-branch=main")
	runGit(t, repoDir, "config", "user.email", "test@example.com")
	runGit(t, repoDir, "config", "user.name", "Test")
	runGit(t, repoDir, "config", "commit.gpgsign", "false")

	for name, content := range files {
		full := filepath.Join(repoDir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, repoDir, "add", ".")
	runGit(t, repoDir, "commit", "-m", "init")

	out, err := exec.Command("git", "-C", repoDir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	sha = string(out[:len(out)-1])
	return "file://" + repoDir, sha
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// upstreamVarsMainYAML is a minimal stand-in for aether-onramp's
// real vars/main.yml. It must include every key onrampPatches()
// targets; patches on missing keys are a hard error.
const upstreamVarsMainYAML = `proxy:
  enabled: false

airgapped:
  enabled: false                 # set true to skip apt update_cache

core:
  standalone: true
  helm:
    local_charts: false
    chart_ref: oci://ghcr.io/omec-project/sd-core
    chart_version: 3.4.0
`

func TestBuildOnramp(t *testing.T) {
	url, sha := setupGitFixture(t, map[string]string{
		"Makefile":      "all:\n\techo onramp\n",
		"README.md":     "# onramp\n",
		"deps/k8s.yml":  "role: k8s\n",
		"vars/main.yml": upstreamVarsMainYAML,
	})

	stageDir := t.TempDir()
	entry, err := BuildOnramp(context.Background(), &bundle.OnrampSpec{
		Repo: url,
		Ref:  "main",
	}, stageDir, "")
	if err != nil {
		t.Fatalf("BuildOnramp: %v", err)
	}

	if entry.ResolvedSHA != sha {
		t.Errorf("ResolvedSHA = %q, want %q", entry.ResolvedSHA, sha)
	}
	if entry.Path != filepath.Join("onramp", "aether-onramp") {
		t.Errorf("Path = %q", entry.Path)
	}
	if len(entry.Files) != 4 {
		t.Errorf("len(Files) = %d, want 4 (skipping .git)", len(entry.Files))
	}

	// Verify content was actually cloned to the staging dir.
	makefile := filepath.Join(stageDir, "onramp", "aether-onramp", "Makefile")
	if _, err := os.Stat(makefile); err != nil {
		t.Errorf("Makefile not in staging: %v", err)
	}
	// .git must not leak into the manifest entries.
	for _, f := range entry.Files {
		if filepath.Base(filepath.Dir(f.Path)) == ".git" {
			t.Errorf("file entry %q is inside .git", f.Path)
		}
	}

	// Assert the post-clone patches landed on disk (manifest only
	// records hashes).
	varsMain, err := os.ReadFile(filepath.Join(stageDir, "onramp", "aether-onramp", "vars", "main.yml"))
	if err != nil {
		t.Fatalf("reading patched vars/main.yml: %v", err)
	}
	got := string(varsMain)

	if !strings.Contains(got, "airgapped:\n  enabled: true") {
		t.Errorf("expected airgapped.enabled=true after BuildOnramp, got:\n%s", got)
	}
	if strings.Contains(got, "airgapped:\n  enabled: false") {
		t.Errorf("airgapped.enabled still false after BuildOnramp:\n%s", got)
	}
	if !strings.Contains(got, "local_charts: true") {
		t.Errorf("expected core.helm.local_charts=true after BuildOnramp, got:\n%s", got)
	}
	if !strings.Contains(got, "chart_ref: "+localSDCoreChartDir) {
		t.Errorf("expected core.helm.chart_ref=%s after BuildOnramp, got:\n%s", localSDCoreChartDir, got)
	}

	if entry.TreeSHA256 == "" {
		t.Errorf("TreeSHA256 should be set, got empty")
	}
	// File entries must be sorted by Path for deterministic manifests.
	for i := 1; i < len(entry.Files); i++ {
		if entry.Files[i-1].Path > entry.Files[i].Path {
			t.Errorf("Files not sorted: %q before %q", entry.Files[i-1].Path, entry.Files[i].Path)
			break
		}
	}
}

func TestBuildOnrampCleansExistingDest(t *testing.T) {
	url, _ := setupGitFixture(t, map[string]string{
		"README.md":     "hi\n",
		"vars/main.yml": upstreamVarsMainYAML,
	})
	stageDir := t.TempDir()

	// Seed a bogus file in the destination to prove clone replaces it.
	destDir := filepath.Join(stageDir, "onramp", "aether-onramp")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destDir, "stale.txt"), []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := BuildOnramp(context.Background(), &bundle.OnrampSpec{Repo: url}, stageDir, ""); err != nil {
		t.Fatalf("BuildOnramp: %v", err)
	}

	if _, err := os.Stat(filepath.Join(destDir, "stale.txt")); !os.IsNotExist(err) {
		t.Errorf("stale file should have been removed by clean clone")
	}
}

func TestBuildOnrampAppliesUserPatches(t *testing.T) {
	url, _ := setupGitFixture(t, map[string]string{
		"vars/main.yml": upstreamVarsMainYAML,
		"ocudu/roles/uEsimulator/templates/ue_zmq.conf": "upstream content\n",
		"ocudu/roles/gNB/templates/gnb_zmq.yaml":        "upstream gnb\n",
	})
	stageDir := t.TempDir()

	// One inline patch and one source-backed patch to cover both paths.
	specDir := t.TempDir()
	srcPath := filepath.Join(specDir, "patches", "gnb_zmq.yaml")
	if err := os.MkdirAll(filepath.Dir(srcPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(srcPath, []byte("operator gnb\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	entry, err := BuildOnramp(context.Background(), &bundle.OnrampSpec{
		Repo: url,
		Patches: []bundle.FilePatch{
			{
				Target:  "ocudu/roles/uEsimulator/templates/ue_zmq.conf",
				Content: "operator ue\n",
			},
			{
				Target: "ocudu/roles/gNB/templates/gnb_zmq.yaml",
				Source: "patches/gnb_zmq.yaml",
			},
		},
	}, stageDir, specDir)
	if err != nil {
		t.Fatalf("BuildOnramp: %v", err)
	}

	// On-disk content must match the patches.
	got1, err := os.ReadFile(filepath.Join(stageDir, entry.Path, "ocudu/roles/uEsimulator/templates/ue_zmq.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got1) != "operator ue\n" {
		t.Errorf("inline patch content = %q, want %q", got1, "operator ue\n")
	}
	got2, err := os.ReadFile(filepath.Join(stageDir, entry.Path, "ocudu/roles/gNB/templates/gnb_zmq.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got2) != "operator gnb\n" {
		t.Errorf("source-backed patch content = %q, want %q", got2, "operator gnb\n")
	}

	// Manifest hashes must reflect the patched bytes, not upstream.
	wantUEHash := sha256Hex([]byte("operator ue\n"))
	wantGNBHash := sha256Hex([]byte("operator gnb\n"))
	for _, f := range entry.Files {
		if strings.HasSuffix(f.Path, "ue_zmq.conf") && f.SHA256 != wantUEHash {
			t.Errorf("ue_zmq.conf manifest hash = %s, want %s", f.SHA256, wantUEHash)
		}
		if strings.HasSuffix(f.Path, "gnb_zmq.yaml") && f.SHA256 != wantGNBHash {
			t.Errorf("gnb_zmq.yaml manifest hash = %s, want %s", f.SHA256, wantGNBHash)
		}
	}

	if entry.TreeSHA256 == "" {
		t.Error("TreeSHA256 should be set on a patched build")
	}
}

func TestBuildOnrampMissingPatchTargetIsHardError(t *testing.T) {
	url, _ := setupGitFixture(t, map[string]string{
		"vars/main.yml": upstreamVarsMainYAML,
	})
	stageDir := t.TempDir()
	_, err := BuildOnramp(context.Background(), &bundle.OnrampSpec{
		Repo: url,
		Patches: []bundle.FilePatch{
			{Target: "does/not/exist.conf", Content: "x"},
		},
	}, stageDir, "")
	if err == nil {
		t.Fatal("expected error when patch target is absent from clone")
	}
	if !strings.Contains(err.Error(), "does/not/exist.conf") {
		t.Errorf("error should mention missing target; got %v", err)
	}
}

func TestBuildHelmCharts(t *testing.T) {
	url1, _ := setupGitFixture(t, map[string]string{
		"Chart.yaml":  "name: amf\nversion: 1.0.0\n",
		"values.yaml": "image: ghcr.io/example/amf:v1\n",
	})
	url2, _ := setupGitFixture(t, map[string]string{
		"Chart.yaml":  "name: smf\nversion: 2.0.0\n",
		"values.yaml": "image: ghcr.io/example/smf:v2\n",
	})

	stageDir := t.TempDir()
	// Empty helm binary — test fixtures have no remote dependencies,
	// so dep resolution is a no-op and we avoid dragging helm into the
	// unit test.
	entries, err := BuildHelmCharts(context.Background(), []bundle.HelmChartsSpec{
		{Name: "amf", Repo: url1},
		{Name: "smf", Repo: url2},
	}, stageDir, "")
	if err != nil {
		t.Fatalf("BuildHelmCharts: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	if entries[0].Name != "amf" || entries[1].Name != "smf" {
		t.Errorf("entry names = %q,%q", entries[0].Name, entries[1].Name)
	}
	for _, e := range entries {
		if e.ResolvedSHA == "" {
			t.Errorf("%s: ResolvedSHA is empty", e.Name)
		}
		chartPath := filepath.Join(stageDir, e.Path, "Chart.yaml")
		if _, err := os.Stat(chartPath); err != nil {
			t.Errorf("%s: Chart.yaml missing: %v", e.Name, err)
		}
	}
}
