package patch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetYAMLFieldFlipsNestedBool(t *testing.T) {
	rootDir := t.TempDir()
	writeFile(t, rootDir, "vars/main.yml", ""+
		"proxy:\n"+
		"  enabled: false\n"+
		"\n"+
		"# airgapped controls apt update_cache gating\n"+
		"airgapped:\n"+
		"  enabled: false                 # flip for offline sites\n"+
		"\n"+
		"core:\n"+
		"  standalone: true\n")

	action := SetYAMLField{
		RelPath: "vars/main.yml",
		KeyPath: []string{"airgapped", "enabled"},
		Value:   true,
	}
	if err := action.Apply(rootDir); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	got := readFile(t, rootDir, "vars/main.yml")

	// The targeted field must flip; sibling blocks must not.
	if !strings.Contains(got, "airgapped:\n  enabled: true") {
		t.Errorf("expected airgapped.enabled=true in output:\n%s", got)
	}
	if !strings.Contains(got, "proxy:\n  enabled: false") {
		t.Errorf("proxy.enabled was unexpectedly changed:\n%s", got)
	}
	if !strings.Contains(got, "standalone: true") {
		t.Errorf("core.standalone was lost:\n%s", got)
	}
}

func TestSetYAMLFieldPreservesLineComment(t *testing.T) {
	rootDir := t.TempDir()
	writeFile(t, rootDir, "vars/main.yml", ""+
		"airgapped:\n"+
		"  enabled: false                 # flip for offline sites\n")

	action := SetYAMLField{
		RelPath: "vars/main.yml",
		KeyPath: []string{"airgapped", "enabled"},
		Value:   true,
	}
	if err := action.Apply(rootDir); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	got := readFile(t, rootDir, "vars/main.yml")
	if !strings.Contains(got, "flip for offline sites") {
		t.Errorf("line comment was dropped:\n%s", got)
	}
}

func TestSetYAMLFieldIdempotent(t *testing.T) {
	rootDir := t.TempDir()
	writeFile(t, rootDir, "vars/main.yml", "airgapped:\n  enabled: true\n")

	action := SetYAMLField{
		RelPath: "vars/main.yml",
		KeyPath: []string{"airgapped", "enabled"},
		Value:   true,
	}
	if err := action.Apply(rootDir); err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	if err := action.Apply(rootDir); err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	got := readFile(t, rootDir, "vars/main.yml")
	if !strings.Contains(got, "enabled: true") {
		t.Errorf("value drifted after re-apply:\n%s", got)
	}
}

func TestSetYAMLFieldUpsertAppendsLeaf(t *testing.T) {
	rootDir := t.TempDir()
	writeFile(t, rootDir, "vars/main.yml", ""+
		"core:\n"+
		"  helm:\n"+
		"    chart_ref: oci://ghcr.io/omec-project/sd-core\n"+
		"    chart_version: 3.5.0\n")

	action := SetYAMLField{
		RelPath: "vars/main.yml",
		KeyPath: []string{"core", "helm", "local_charts"},
		Value:   true,
		Upsert:  true,
	}
	if err := action.Apply(rootDir); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	got := readFile(t, rootDir, "vars/main.yml")
	if !strings.Contains(got, "local_charts: true") {
		t.Errorf("expected appended local_charts=true:\n%s", got)
	}
	// Existing siblings must survive.
	if !strings.Contains(got, "chart_ref: oci://ghcr.io/omec-project/sd-core") {
		t.Errorf("sibling chart_ref was lost:\n%s", got)
	}
	if !strings.Contains(got, "chart_version: 3.5.0") {
		t.Errorf("sibling chart_version was lost:\n%s", got)
	}
}

func TestSetYAMLFieldUpsertIsIdempotent(t *testing.T) {
	rootDir := t.TempDir()
	writeFile(t, rootDir, "vars/main.yml", ""+
		"core:\n"+
		"  helm:\n"+
		"    chart_ref: oci://example/chart\n")

	action := SetYAMLField{
		RelPath: "vars/main.yml",
		KeyPath: []string{"core", "helm", "local_charts"},
		Value:   true,
		Upsert:  true,
	}
	if err := action.Apply(rootDir); err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	if err := action.Apply(rootDir); err != nil {
		t.Fatalf("second Apply: %v", err)
	}

	got := readFile(t, rootDir, "vars/main.yml")
	if n := strings.Count(got, "local_charts:"); n != 1 {
		t.Errorf("expected exactly one local_charts entry after re-apply, got %d:\n%s", n, got)
	}
}

func TestSetYAMLFieldUpsertStillErrorsOnMissingIntermediate(t *testing.T) {
	rootDir := t.TempDir()
	writeFile(t, rootDir, "vars/main.yml", "proxy:\n  enabled: false\n")

	action := SetYAMLField{
		RelPath: "vars/main.yml",
		KeyPath: []string{"core", "helm", "local_charts"},
		Value:   true,
		Upsert:  true,
	}
	err := action.Apply(rootDir)
	if err == nil {
		t.Fatal("expected error when intermediate key is missing even with Upsert; got nil")
	}
	if !strings.Contains(err.Error(), "core") {
		t.Errorf("error should mention the missing intermediate key; got %v", err)
	}
}

func TestSetYAMLFieldUpsertNameDistinguishesOp(t *testing.T) {
	a := SetYAMLField{RelPath: "f.yml", KeyPath: []string{"x"}, Value: 1}
	b := SetYAMLField{RelPath: "f.yml", KeyPath: []string{"x"}, Value: 1, Upsert: true}
	if !strings.HasPrefix(a.Name(), "set ") {
		t.Errorf("strict Name should start with 'set ', got %q", a.Name())
	}
	if !strings.HasPrefix(b.Name(), "upsert ") {
		t.Errorf("upsert Name should start with 'upsert ', got %q", b.Name())
	}
}

func TestSetYAMLFieldErrorsOnMissingKey(t *testing.T) {
	rootDir := t.TempDir()
	writeFile(t, rootDir, "vars/main.yml", "proxy:\n  enabled: false\n")

	action := SetYAMLField{
		RelPath: "vars/main.yml",
		KeyPath: []string{"airgapped", "enabled"},
		Value:   true,
	}
	err := action.Apply(rootDir)
	if err == nil {
		t.Fatal("expected error when top-level key is missing; got nil")
	}
	if !strings.Contains(err.Error(), "airgapped") {
		t.Errorf("error should mention the missing key; got %v", err)
	}
}

func TestSetYAMLFieldErrorsOnMissingFile(t *testing.T) {
	rootDir := t.TempDir()
	action := SetYAMLField{
		RelPath: "vars/main.yml",
		KeyPath: []string{"x"},
		Value:   1,
	}
	if err := action.Apply(rootDir); err == nil {
		t.Fatal("expected error when target file is missing")
	}
}

func TestApplyAllWrapsErrorWithName(t *testing.T) {
	rootDir := t.TempDir()
	writeFile(t, rootDir, "vars/main.yml", "proxy:\n  enabled: false\n")

	actions := []Action{
		SetYAMLField{RelPath: "vars/main.yml", KeyPath: []string{"missing"}, Value: true},
	}
	err := ApplyAll(rootDir, actions)
	if err == nil {
		t.Fatal("expected ApplyAll to surface action error")
	}
	if !strings.Contains(err.Error(), "set vars/main.yml:missing=true") {
		t.Errorf("error should include action name; got %v", err)
	}
}

func writeFile(t *testing.T, rootDir, rel, content string) {
	t.Helper()
	full := filepath.Join(rootDir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, rootDir, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(rootDir, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
