package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// SchemaVersion is the current state file schema version.
// The launcher refuses to read state files with unrecognized schema versions.
const SchemaVersion = 1

// DefaultPath is the default location of the state file on disk.
const DefaultPath = "/var/lib/aether-ops-bootstrap/state.json"

// State is the runtime state of the bootstrap process, persisted to disk.
// It records what was installed, when, and by which launcher/bundle versions.
type State struct {
	SchemaVersion   int                       `json:"schema_version"`
	LauncherVersion string                    `json:"launcher_version"`
	BundleVersion   string                    `json:"bundle_version"`
	BundleHash      string                    `json:"bundle_hash"`
	Components      map[string]ComponentState `json:"components"`
	History         []HistoryEntry            `json:"history"`
}

// ComponentState records the installed state of a single component.
// The ConfigHash lets upgrade detect drift when templates change between
// bundle versions without the component binary itself changing.
type ComponentState struct {
	Version     string    `json:"version"`
	ConfigHash  string    `json:"config_hash"`
	InstalledAt time.Time `json:"installed_at"`
}

// HistoryEntry is an append-only record of an action taken by the launcher.
// The history array gives support engineers a forensic trail.
type HistoryEntry struct {
	Action          string    `json:"action"`
	Timestamp       time.Time `json:"timestamp"`
	LauncherVersion string    `json:"launcher_version"`
	BundleVersion   string    `json:"bundle_version"`
}

// ErrSchemaVersion is returned when a state file has an unrecognized schema version.
type ErrSchemaVersion struct {
	Got  int
	Want int
}

func (e *ErrSchemaVersion) Error() string {
	return fmt.Sprintf("unsupported state schema version %d (expected %d)", e.Got, e.Want)
}

// Read loads state from a JSON file at the given path.
// Returns a non-nil error if the file is missing, invalid, or has an
// unrecognized schema version.
func Read(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading state %s: %w", path, err)
	}

	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parsing state %s: %w", path, err)
	}

	if s.SchemaVersion != SchemaVersion {
		return nil, &ErrSchemaVersion{Got: s.SchemaVersion, Want: SchemaVersion}
	}

	return &s, nil
}

// Write atomically persists state to a JSON file at the given path.
// It writes to a temporary file in the same directory and renames, so
// the state file is never partially written.
func Write(path string, s *State) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating state directory %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".state-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp file for state: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("writing temp state file: %w", err)
	}

	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("closing temp state file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("renaming temp state file to %s: %w", path, err)
	}

	return nil
}
