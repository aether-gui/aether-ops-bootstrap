package state

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStateRoundTrip(t *testing.T) {
	now := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
	s := &State{
		SchemaVersion:   SchemaVersion,
		LauncherVersion: "0.1.0",
		BundleVersion:   "2026.04.1",
		BundleHash:      "abc123",
		Components: map[string]ComponentState{
			"rke2": {
				Version:     "v1.33.1+rke2r1",
				ConfigHash:  "def456",
				InstalledAt: now,
			},
		},
		History: []HistoryEntry{
			{
				Action:          "install",
				Timestamp:       now,
				LauncherVersion: "0.1.0",
				BundleVersion:   "2026.04.1",
			},
		},
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	if err := Write(path, s); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if got.SchemaVersion != s.SchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", got.SchemaVersion, s.SchemaVersion)
	}
	if got.LauncherVersion != s.LauncherVersion {
		t.Errorf("LauncherVersion = %q, want %q", got.LauncherVersion, s.LauncherVersion)
	}
	if got.BundleVersion != s.BundleVersion {
		t.Errorf("BundleVersion = %q, want %q", got.BundleVersion, s.BundleVersion)
	}

	cs, ok := got.Components["rke2"]
	if !ok {
		t.Fatal("Components[rke2] not found")
	}
	if cs.Version != "v1.33.1+rke2r1" {
		t.Errorf("rke2 version = %q, want %q", cs.Version, "v1.33.1+rke2r1")
	}

	if len(got.History) != 1 {
		t.Fatalf("len(History) = %d, want 1", len(got.History))
	}
	if got.History[0].Action != "install" {
		t.Errorf("History[0].Action = %q, want %q", got.History[0].Action, "install")
	}
}

func TestReadMissingFile(t *testing.T) {
	_, err := Read("/nonexistent/path/state.json")
	if err == nil {
		t.Fatal("Read with missing file should return error")
	}
}

func TestReadInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	_ = os.WriteFile(path, []byte("{invalid"), 0644)

	_, err := Read(path)
	if err == nil {
		t.Fatal("Read with invalid JSON should return error")
	}
}

func TestReadSchemaVersionMismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	_ = os.WriteFile(path, []byte(`{"schema_version": 999}`), 0644)

	_, err := Read(path)
	if err == nil {
		t.Fatal("Read with wrong schema version should return error")
	}

	var schemaErr *ErrSchemaVersion
	if !errors.As(err, &schemaErr) {
		t.Fatalf("error should be *ErrSchemaVersion, got %T: %v", err, err)
	}
	if schemaErr.Got != 999 {
		t.Errorf("ErrSchemaVersion.Got = %d, want 999", schemaErr.Got)
	}
	if schemaErr.Want != SchemaVersion {
		t.Errorf("ErrSchemaVersion.Want = %d, want %d", schemaErr.Want, SchemaVersion)
	}
}

func TestWriteAtomicity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	s := &State{SchemaVersion: SchemaVersion, LauncherVersion: "0.1.0"}
	if err := Write(path, s); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Verify no temp files remain.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "state.json" {
			t.Errorf("unexpected file remaining: %s", e.Name())
		}
	}
}

func TestWriteCreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "dir", "state.json")

	s := &State{SchemaVersion: SchemaVersion}
	if err := Write(path, s); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Errorf("state file should exist at %s: %v", path, err)
	}
}
