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
// Intermediate keys must resolve to mappings; a missing intermediate
// key is always a hard error. A missing leaf key is a hard error too
// unless Upsert is set, in which case the leaf is appended.
//
// Strict-by-default exists so upstream renames break the build
// instead of producing a bundle with stale defaults. Upsert is for
// fields that are legitimately optional in a given file (for example
// blueprint vars files that omit a Boolean whose role-side default
// is false) — the canonical reference file should still be patched
// strictly so renames stay loud.
//
// Rewrite goes through yaml.v3's Node API so comments on unchanged
// nodes survive.
type SetYAMLField struct {
	RelPath string   // file to patch, relative to Apply's rootDir
	KeyPath []string // non-empty sequence of mapping keys to descend
	Value   any      // any type yaml.v3 encodes as a scalar
	Upsert  bool     // when true, append the leaf key if absent instead of erroring
}

func (s SetYAMLField) Name() string {
	op := "set"
	if s.Upsert {
		op = "upsert"
	}
	return fmt.Sprintf("%s %s:%s=%v", op, s.RelPath, strings.Join(s.KeyPath, "."), s.Value)
}

// Target returns the file the action modifies.
func (s SetYAMLField) Target() string { return s.RelPath }

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

	if err := setMappingPath(&root, s.KeyPath, s.Value, s.Upsert); err != nil {
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
//
// When upsert is true and the leaf key is absent, it is appended to
// the parent mapping. Intermediate keys must always exist; we never
// auto-create nested mappings.
func setMappingPath(root *yaml.Node, keyPath []string, value any, upsert bool) error {
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

		isLeaf := i == len(keyPath)-1

		if idx < 0 {
			if isLeaf && upsert {
				keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k}
				var valNode yaml.Node
				if err := valNode.Encode(value); err != nil {
					return fmt.Errorf("encoding value for %q: %w", strings.Join(keyPath, "."), err)
				}
				n.Content = append(n.Content, keyNode, &valNode)
				return nil
			}
			return fmt.Errorf("key %q not found", strings.Join(keyPath[:i+1], "."))
		}

		if isLeaf {
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
