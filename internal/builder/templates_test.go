package builder

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStageTemplates(t *testing.T) {
	srcDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(srcDir, "sshd_config.d"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "sshd_config.d", "99-aether.conf"), []byte("PermitRootLogin no\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "rke2-config.yaml.tmpl"), []byte("node-name: {{.Hostname}}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	stageDir := t.TempDir()
	entry, err := StageTemplates(srcDir, stageDir)
	if err != nil {
		t.Fatalf("StageTemplates: %v", err)
	}
	if entry == nil {
		t.Fatal("entry is nil, expected templates")
	}
	if len(entry.Files) != 2 {
		t.Fatalf("len(Files) = %d, want 2", len(entry.Files))
	}

	// Verify files exist in staging.
	for _, f := range entry.Files {
		p := filepath.Join(stageDir, filepath.FromSlash(f.Path))
		if _, err := os.Stat(p); err != nil {
			t.Errorf("missing staged file %s: %v", f.Path, err)
		}
		if f.SHA256 == "" {
			t.Errorf("empty SHA256 for %s", f.Path)
		}
		if f.Size == 0 {
			t.Errorf("zero size for %s", f.Path)
		}
	}
}

func TestStageTemplatesEmpty(t *testing.T) {
	srcDir := t.TempDir()
	// Only .gitkeep.
	if err := os.WriteFile(filepath.Join(srcDir, ".gitkeep"), nil, 0644); err != nil {
		t.Fatal(err)
	}

	stageDir := t.TempDir()
	entry, err := StageTemplates(srcDir, stageDir)
	if err != nil {
		t.Fatalf("StageTemplates: %v", err)
	}
	if entry != nil {
		t.Errorf("expected nil for empty templates, got %+v", entry)
	}
}
