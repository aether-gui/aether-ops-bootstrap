package builder

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestFindChartsNeedingDeps(t *testing.T) {
	root := t.TempDir()

	// Chart A: remote (https) dep → should be reported.
	writeChart(t, root, "a", `
apiVersion: v2
name: a
version: 1.0.0
dependencies:
  - name: mongodb
    version: 1.0.0
    repository: https://charts.bitnami.com/bitnami
`)

	// Chart B: only file:// dep → should be skipped (resolves from
	// sibling charts at install time).
	writeChart(t, root, "b", `
apiVersion: v2
name: b
version: 1.0.0
dependencies:
  - name: a
    version: 1.0.0
    repository: file://../a
`)

	// Chart C: no dependencies → skipped.
	writeChart(t, root, "c", `
apiVersion: v2
name: c
version: 1.0.0
`)

	// Chart D: oci:// dep → should be reported.
	writeChart(t, root, "d", `
apiVersion: v2
name: d
version: 1.0.0
dependencies:
  - name: something
    version: 1.0.0
    repository: oci://ghcr.io/example/something
`)

	// .git/Chart.yaml deliberately inside a .git dir to prove the
	// walker skips it.
	writeChart(t, root, ".git/should-not-be-scanned", `
apiVersion: v2
name: ignored
version: 1.0.0
dependencies:
  - name: shouldnt-matter
    version: 1.0.0
    repository: https://example.com/ignored
`)

	got, err := findChartsNeedingDeps(root)
	if err != nil {
		t.Fatalf("findChartsNeedingDeps: %v", err)
	}

	want := []string{filepath.Join(root, "a"), filepath.Join(root, "d")}
	sort.Strings(got)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("found %d charts, want %d\n got: %v\nwant: %v", len(got), len(want), got, want)
	}
	for i, g := range got {
		if g != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, g, want[i])
		}
	}
}

func TestResolveChartDependenciesSkipsWhenHelmBinaryEmpty(t *testing.T) {
	// When helmBinary is empty, resolution is a no-op even if the
	// tree has a chart with remote deps.
	root := t.TempDir()
	writeChart(t, root, "a", `
apiVersion: v2
name: a
version: 1.0.0
dependencies:
  - name: mongodb
    version: 1.0.0
    repository: https://charts.bitnami.com/bitnami
`)

	if err := resolveChartDependencies(context.Background(), "", root); err != nil {
		t.Errorf("expected no-op with empty helmBinary, got: %v", err)
	}

	// The skipped path must not leave a charts/ subdir behind.
	if _, err := os.Stat(filepath.Join(root, "a", "charts")); !os.IsNotExist(err) {
		t.Errorf("expected no charts/ dir created with empty helmBinary")
	}
}

func writeChart(t *testing.T, root, name, yaml string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Chart.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
}
