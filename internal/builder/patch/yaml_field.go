package patch

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// SetYAMLField sets a scalar mapping field in a YAML file to Value,
// descending through KeyPath (e.g. ["airgapped", "enabled"]).
// Intermediate keys must resolve to mappings; any missing key is a
// hard error so upstream renames break the build instead of producing
// a bundle with stale defaults.
//
// Rewrite goes through yaml.v3's Node API so comments on unchanged
// nodes survive.
type SetYAMLField struct {
	RelPath string   // file to patch, relative to Apply's rootDir
	KeyPath []string // non-empty sequence of mapping keys to descend
	Value   any      // any type yaml.v3 encodes as a scalar
}

func (s SetYAMLField) Name() string {
	return fmt.Sprintf("set %s:%s=%v", s.RelPath, strings.Join(s.KeyPath, "."), s.Value)
}

func (s SetYAMLField) Apply(rootDir string) error {
	if len(s.KeyPath) == 0 {
		return errors.New("KeyPath is empty")
	}

	full := filepath.Join(rootDir, s.RelPath)
	raw, err := os.ReadFile(full)
	if err != nil {
		return fmt.Errorf("reading %s: %w", full, err)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return fmt.Errorf("parsing %s: %w", full, err)
	}

	if err := setMappingPath(&root, s.KeyPath, s.Value); err != nil {
		return fmt.Errorf("in %s: %w", full, err)
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&root); err != nil {
		return fmt.Errorf("encoding %s: %w", full, err)
	}
	if err := enc.Close(); err != nil {
		return err
	}

	mode := fs.FileMode(0644)
	if info, err := os.Stat(full); err == nil {
		mode = info.Mode()
	}
	return os.WriteFile(full, buf.Bytes(), mode)
}

// setMappingPath walks root by mapping keys and replaces the value
// at the final key with an encoding of value. Comments attached to
// the existing value node are carried over to the new one so the
// surrounding file reads the same after the edit.
func setMappingPath(root *yaml.Node, keyPath []string, value any) error {
	n := root
	if n.Kind == yaml.DocumentNode {
		if len(n.Content) == 0 {
			return errors.New("empty document")
		}
		n = n.Content[0]
	}

	for i, k := range keyPath {
		if n.Kind != yaml.MappingNode {
			return fmt.Errorf("segment %q: expected mapping, got node kind %d", strings.Join(keyPath[:i], "."), n.Kind)
		}

		idx := -1
		for j := 0; j < len(n.Content)-1; j += 2 {
			if n.Content[j].Value == k {
				idx = j
				break
			}
		}
		if idx < 0 {
			return fmt.Errorf("key %q not found", strings.Join(keyPath[:i+1], "."))
		}

		if i == len(keyPath)-1 {
			var newVal yaml.Node
			if err := newVal.Encode(value); err != nil {
				return fmt.Errorf("encoding value for %q: %w", strings.Join(keyPath, "."), err)
			}
			// Preserve comments from the replaced node so the
			// rewritten file reads like the original minus the
			// value change.
			old := n.Content[idx+1]
			newVal.HeadComment = old.HeadComment
			newVal.LineComment = old.LineComment
			newVal.FootComment = old.FootComment
			n.Content[idx+1] = &newVal
			return nil
		}

		n = n.Content[idx+1]
	}
	return nil
}
