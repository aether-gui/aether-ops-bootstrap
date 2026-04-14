package diagnostics

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCollect_CreatesValidTarball(t *testing.T) {
	outputDir := t.TempDir()

	path, err := Collect(outputDir, CollectOpts{
		StateFile: "/nonexistent/state.json",
	})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("tarball not created at %s: %v", path, err)
	}

	if !strings.HasSuffix(path, ".tar.gz") {
		t.Errorf("tarball name %q should end in .tar.gz", path)
	}

	// Verify it's a valid tar.gz with expected entries.
	entries := listTarGzEntries(t, path)
	if !containsEntry(entries, "diag-meta.json") {
		t.Error("tarball should contain diag-meta.json")
	}
	if !containsEntryPrefix(entries, "system/") {
		t.Error("tarball should contain system/ entries")
	}
}

func TestCollect_IncludesSystemInfo(t *testing.T) {
	outputDir := t.TempDir()

	path, err := Collect(outputDir, CollectOpts{})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	entries := listTarGzEntries(t, path)
	for _, expected := range []string{"system/hostname.txt", "system/uname.txt", "system/disk.txt"} {
		if !containsEntry(entries, expected) {
			t.Errorf("tarball should contain %s", expected)
		}
	}
}

func TestCollect_MissingStateFile(t *testing.T) {
	outputDir := t.TempDir()

	path, err := Collect(outputDir, CollectOpts{
		StateFile: "/nonexistent/state.json",
	})
	if err != nil {
		t.Fatalf("Collect should succeed even with missing state: %v", err)
	}

	// state/ dir should still exist but without state.json.
	entries := listTarGzEntries(t, path)
	if containsEntry(entries, "state/state.json") {
		t.Error("should not contain state.json when file doesn't exist")
	}
}

func TestCollect_MissingLogFile(t *testing.T) {
	outputDir := t.TempDir()

	path, err := Collect(outputDir, CollectOpts{
		LogFile: "/nonexistent/bootstrap.log",
	})
	if err != nil {
		t.Fatalf("Collect should succeed even with missing log: %v", err)
	}

	entries := listTarGzEntries(t, path)
	if containsEntry(entries, "state/bootstrap.log") {
		t.Error("should not contain bootstrap.log when file doesn't exist")
	}
}

func TestCollect_IncludesLogFile(t *testing.T) {
	outputDir := t.TempDir()
	logFile := filepath.Join(t.TempDir(), "test.log")
	if err := os.WriteFile(logFile, []byte("test log output\n"), 0644); err != nil {
		t.Fatal(err)
	}

	path, err := Collect(outputDir, CollectOpts{
		LogFile: logFile,
	})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	entries := listTarGzEntries(t, path)
	if !containsEntry(entries, "state/bootstrap.log") {
		t.Error("tarball should contain state/bootstrap.log")
	}
}

func TestCollect_IncludesStateFile(t *testing.T) {
	outputDir := t.TempDir()
	stateFile := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(stateFile, []byte(`{"schema_version":1}`), 0644); err != nil {
		t.Fatal(err)
	}

	path, err := Collect(outputDir, CollectOpts{
		StateFile: stateFile,
	})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	entries := listTarGzEntries(t, path)
	if !containsEntry(entries, "state/state.json") {
		t.Error("tarball should contain state/state.json")
	}
}

func TestCollect_RecordsVersion(t *testing.T) {
	outputDir := t.TempDir()

	path, err := Collect(outputDir, CollectOpts{
		Version: "v1.2.3",
	})
	if err != nil {
		t.Fatal(err)
	}

	content := readTarGzEntry(t, path, "diag-meta.json")
	if !strings.Contains(content, "v1.2.3") {
		t.Errorf("diag-meta.json should contain version, got: %s", content)
	}
}

func TestRunCommand_Success(t *testing.T) {
	out := runCommand("echo", "hello")
	if !strings.Contains(out, "hello") {
		t.Errorf("runCommand(echo hello) = %q", out)
	}
}

func TestRunCommand_MissingBinary(t *testing.T) {
	out := runCommand("nonexistent-command-xyzzy")
	if !strings.Contains(out, "error") {
		t.Errorf("missing binary should produce error text, got: %q", out)
	}
}

func TestCopyFileIfExists_Missing(t *testing.T) {
	dir := t.TempDir()
	copyFileIfExists("/nonexistent/file", dir, "test.txt")

	if _, err := os.Stat(filepath.Join(dir, "test.txt")); !os.IsNotExist(err) {
		t.Error("should not create file for missing source")
	}
}

func TestCopyFileIfExists_Present(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src.txt")
	if err := os.WriteFile(src, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	copyFileIfExists(src, dir, "dst.txt")

	data, err := os.ReadFile(filepath.Join(dir, "dst.txt"))
	if err != nil {
		t.Fatalf("dst.txt should exist: %v", err)
	}
	if string(data) != "data" {
		t.Errorf("content = %q, want %q", string(data), "data")
	}
}

func TestWriteFile_CreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	if err := writeFile(dir, "a/b/c.txt", []byte("deep")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "a", "b", "c.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "deep" {
		t.Errorf("content = %q", string(data))
	}
}

// --- Test helpers ---

func listTarGzEntries(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open tarball: %v", err)
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	var entries []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar next: %v", err)
		}
		entries = append(entries, hdr.Name)
	}
	return entries
}

func readTarGzEntry(t *testing.T, path, entryName string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			t.Fatalf("entry %q not found in tarball", entryName)
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Name == entryName {
			data, err := io.ReadAll(tr)
			if err != nil {
				t.Fatal(err)
			}
			return string(data)
		}
	}
}

func containsEntry(entries []string, name string) bool {
	for _, e := range entries {
		if e == name {
			return true
		}
	}
	return false
}

func containsEntryPrefix(entries []string, prefix string) bool {
	for _, e := range entries {
		if strings.HasPrefix(e, prefix) {
			return true
		}
	}
	return false
}
