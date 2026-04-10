package builder

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
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

func TestBuildOnramp(t *testing.T) {
	url, sha := setupGitFixture(t, map[string]string{
		"Makefile":     "all:\n\techo onramp\n",
		"README.md":    "# onramp\n",
		"deps/k8s.yml": "role: k8s\n",
	})

	stageDir := t.TempDir()
	entry, err := BuildOnramp(context.Background(), &bundle.OnrampSpec{
		Repo: url,
		Ref:  "main",
	}, stageDir)
	if err != nil {
		t.Fatalf("BuildOnramp: %v", err)
	}

	if entry.ResolvedSHA != sha {
		t.Errorf("ResolvedSHA = %q, want %q", entry.ResolvedSHA, sha)
	}
	if entry.Path != filepath.Join("onramp", "aether-onramp") {
		t.Errorf("Path = %q", entry.Path)
	}
	if len(entry.Files) != 3 {
		t.Errorf("len(Files) = %d, want 3 (skipping .git)", len(entry.Files))
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
}

func TestBuildOnrampCleansExistingDest(t *testing.T) {
	url, _ := setupGitFixture(t, map[string]string{"README.md": "hi\n"})
	stageDir := t.TempDir()

	// Seed a bogus file in the destination to prove clone replaces it.
	destDir := filepath.Join(stageDir, "onramp", "aether-onramp")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destDir, "stale.txt"), []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := BuildOnramp(context.Background(), &bundle.OnrampSpec{Repo: url}, stageDir); err != nil {
		t.Fatalf("BuildOnramp: %v", err)
	}

	if _, err := os.Stat(filepath.Join(destDir, "stale.txt")); !os.IsNotExist(err) {
		t.Errorf("stale file should have been removed by clean clone")
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
	entries, err := BuildHelmCharts(context.Background(), []bundle.HelmChartsSpec{
		{Name: "amf", Repo: url1},
		{Name: "smf", Repo: url2},
	}, stageDir)
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
