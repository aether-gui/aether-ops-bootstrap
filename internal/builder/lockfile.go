package builder

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
)

// Lockfile pins resolved deb versions for reproducible builds.
// Keyed by "suite/arch", then by package name.
type Lockfile struct {
	SchemaVersion int                             `json:"schema_version"`
	Debs          map[string]map[string]LockEntry `json:"debs"`
}

// LockEntry records the pinned version and hash for a single package.
type LockEntry struct {
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

// ReadLockfile reads and parses a lockfile. Returns (nil, nil) if the
// file doesn't exist or contains only "{}".
func ReadLockfile(path string) (*Lockfile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading lockfile %s: %w", path, err)
	}

	trimmed := strings.TrimSpace(string(data))
	if trimmed == "{}" || trimmed == "" {
		return nil, nil
	}

	var lf Lockfile
	if err := json.Unmarshal(data, &lf); err != nil {
		return nil, fmt.Errorf("parsing lockfile %s: %w", path, err)
	}

	return &lf, nil
}

// WriteLockfile atomically writes a lockfile to the given path.
func WriteLockfile(path string, lf *Lockfile) error {
	data, err := json.MarshalIndent(lf, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling lockfile: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".lock-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}

	return nil
}

// BuildLockfile constructs a lockfile from resolved deb entries.
func BuildLockfile(debEntries []bundle.DebEntry) *Lockfile {
	lf := &Lockfile{
		SchemaVersion: 1,
		Debs:          make(map[string]map[string]LockEntry),
	}

	for _, d := range debEntries {
		key := d.Suite + "/" + d.Arch
		if lf.Debs[key] == nil {
			lf.Debs[key] = make(map[string]LockEntry)
		}
		lf.Debs[key][d.Name] = LockEntry{
			Version: d.Version,
			SHA256:  d.SHA256,
		}
	}

	return lf
}

// VerifyLockfile compares an existing lockfile to a current one.
// Returns nil if they match. Returns a descriptive error listing
// mismatches if packages changed version or hash.
func VerifyLockfile(existing, current *Lockfile) error {
	var diffs []string

	for key, existingPkgs := range existing.Debs {
		currentPkgs, ok := current.Debs[key]
		if !ok {
			diffs = append(diffs, fmt.Sprintf("  %s: section missing from current build", key))
			continue
		}

		for name, existingEntry := range existingPkgs {
			currentEntry, ok := currentPkgs[name]
			if !ok {
				// Package removed from resolution — could be normal dep tree change.
				continue
			}
			if currentEntry.Version != existingEntry.Version {
				diffs = append(diffs, fmt.Sprintf("  %s/%s: version changed %s -> %s",
					key, name, existingEntry.Version, currentEntry.Version))
			} else if currentEntry.SHA256 != existingEntry.SHA256 {
				diffs = append(diffs, fmt.Sprintf("  %s/%s: hash changed (same version %s)",
					key, name, existingEntry.Version))
			}
		}
	}

	if len(diffs) == 0 {
		return nil
	}

	sort.Strings(diffs)
	return fmt.Errorf("lockfile drift detected:\n%s", strings.Join(diffs, "\n"))
}
