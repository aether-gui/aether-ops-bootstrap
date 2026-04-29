// Command patch-bundle rewrites files inside an existing bundle.tar.zst
// without performing a full bundle rebuild.
//
// It extracts the bundle to a temporary directory, applies a set of
// file replacements against the bundled aether-onramp tree, recomputes
// the onramp manifest entry's per-file hashes plus tree_sha256, and
// re-archives the result. The manifest's tree_sha256 change is what
// makes the launcher's onramp component detect the patched bundle as
// distinct from the unpatched one even though the upstream resolved
// commit is identical (see internal/components/onramp/onramp.go's
// composeVersion).
//
// Usage:
//
//	patch-bundle --in <bundle.tar.zst> --out <patched.tar.zst> \
//	  --replace <onramp-rel-path>=<local-file> [--replace ...]
//
//	patch-bundle --in <bundle.tar.zst> --out <patched.tar.zst> \
//	  --patches patches.yaml
//
// `patches.yaml` mirrors the `onramp.patches:` block of specs/bundle.yaml.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/aether-gui/aether-ops-bootstrap/internal/builder"
	"github.com/aether-gui/aether-ops-bootstrap/internal/builder/patch"
	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
)

func main() {
	err := run(os.Args[1:], os.Stdout, os.Stderr)
	if err == nil {
		return
	}
	// flag.ContinueOnError returns flag.ErrHelp on -help; the flag
	// package already printed usage to stderr, so exit cleanly with no
	// extra noise on the stderr line.
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	fmt.Fprintln(os.Stderr, "patch-bundle:", err)
	os.Exit(1)
}

// replaceFlag accumulates repeated --replace KEY=PATH values.
type replaceFlag []string

func (r *replaceFlag) String() string     { return strings.Join(*r, ",") }
func (r *replaceFlag) Set(v string) error { *r = append(*r, v); return nil }

type patchesFile struct {
	SchemaVersion int                `yaml:"schema_version"`
	Patches       []bundle.FilePatch `yaml:"patches"`
}

func run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("patch-bundle", flag.ContinueOnError)
	fs.SetOutput(stderr)

	in := fs.String("in", "", "input bundle.tar.zst (required)")
	out := fs.String("out", "", "output bundle.tar.zst path (required unless --output-dir)")
	outDir := fs.String("output-dir", "", "directory to write <basename>-patched.tar.zst into; ignored if --out is set")
	patchesPath := fs.String("patches", "", "path to a YAML file declaring patches; mutually exclusive with --replace")
	keepWorkdir := fs.Bool("keep-workdir", false, "keep the extraction temp directory for inspection (debug)")
	var replaces replaceFlag
	fs.Var(&replaces, "replace", "override a file in the onramp tree: <onramp-rel-path>=<local-file> (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *in == "" {
		return errors.New("--in is required")
	}
	if len(replaces) == 0 && *patchesPath == "" {
		return errors.New("must provide --replace and/or --patches")
	}
	if len(replaces) > 0 && *patchesPath != "" {
		return errors.New("--replace and --patches are mutually exclusive")
	}

	outPath, err := resolveOutPath(*in, *out, *outDir)
	if err != nil {
		return err
	}

	workDir, err := os.MkdirTemp("", "patch-bundle-*")
	if err != nil {
		return fmt.Errorf("creating workdir: %w", err)
	}
	if !*keepWorkdir {
		defer os.RemoveAll(workDir)
	} else {
		fmt.Fprintf(stdout, "keeping workdir at %s\n", workDir)
	}

	fmt.Fprintf(stdout, "extracting %s ...\n", *in)
	if err := builder.Unarchive(*in, workDir); err != nil {
		return fmt.Errorf("extracting bundle: %w", err)
	}

	manifestPath := filepath.Join(workDir, "manifest.json")
	manifest, err := bundle.Read(manifestPath)
	if err != nil {
		return fmt.Errorf("reading manifest: %w", err)
	}
	if manifest.Components.Onramp == nil {
		return errors.New("input bundle has no onramp component; nothing to patch")
	}
	onrampPath := manifest.Components.Onramp.Path
	if onrampPath == "" {
		return errors.New("manifest onramp.path is empty")
	}

	patches, baseDir, err := loadPatches(replaces, *patchesPath)
	if err != nil {
		return err
	}
	if err := bundle.ValidateFilePatches(patches, "patches"); err != nil {
		return err
	}

	actions, err := builder.BuildFilePatchActions(patches, baseDir)
	if err != nil {
		return fmt.Errorf("preparing patches: %w", err)
	}

	onrampDir := filepath.Join(workDir, onrampPath)
	if err := patch.ApplyAll(onrampDir, actions); err != nil {
		return fmt.Errorf("applying patches: %w", err)
	}
	for _, p := range patches {
		fmt.Fprintf(stdout, "  patched %s\n", p.Target)
	}

	files, err := builder.HashTree(onrampDir, onrampPath)
	if err != nil {
		return fmt.Errorf("rehashing onramp tree: %w", err)
	}
	manifest.Components.Onramp.Files = files
	manifest.Components.Onramp.TreeSHA256 = bundle.ComputeTreeSHA256(files)
	// BundleSHA256 is informational; the new sidecar file is the
	// authoritative integrity record once we re-archive. Clear so
	// stale-looking values don't ship in the patched manifest.
	manifest.BundleSHA256 = ""

	if err := bundle.Write(manifestPath, manifest); err != nil {
		return fmt.Errorf("writing manifest: %w", err)
	}

	tmpPath := outPath + ".tmp"
	fmt.Fprintf(stdout, "archiving to %s ...\n", outPath)
	if err := builder.Archive(workDir, tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("archiving: %w", err)
	}
	if err := os.Rename(tmpPath, outPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("renaming output: %w", err)
	}

	hash, err := builder.WriteBundleChecksum(outPath)
	if err != nil {
		return fmt.Errorf("writing checksum: %w", err)
	}

	fmt.Fprintf(stdout, "wrote %s\n", outPath)
	fmt.Fprintf(stdout, "wrote %s\n", outPath+".sha256")
	fmt.Fprintf(stdout, "bundle sha256: %s\n", hash)
	fmt.Fprintf(stdout, "patched %d file(s); new tree_sha256: %s\n",
		len(patches), shortHash(manifest.Components.Onramp.TreeSHA256))
	return nil
}

// resolveOutPath picks the output path from --out / --output-dir and
// rejects in-place overwrites that could be issued by accident.
func resolveOutPath(in, out, outDir string) (string, error) {
	switch {
	case out != "":
		// fine
	case outDir != "":
		base := strings.TrimSuffix(filepath.Base(in), ".tar.zst")
		out = filepath.Join(outDir, base+"-patched.tar.zst")
	default:
		return "", errors.New("--out or --output-dir is required")
	}
	absIn, err := filepath.Abs(in)
	if err != nil {
		return "", fmt.Errorf("resolving --in: %w", err)
	}
	absOut, err := filepath.Abs(out)
	if err != nil {
		return "", fmt.Errorf("resolving --out: %w", err)
	}
	if absIn == absOut {
		return "", errors.New("output must differ from input; refusing to overwrite the source bundle in place")
	}
	parent := filepath.Dir(absOut)
	info, err := os.Stat(parent)
	if err != nil {
		return "", fmt.Errorf("output parent directory: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("output parent %s is not a directory", parent)
	}
	return out, nil
}

// loadPatches builds the FilePatch slice from either a --patches YAML
// file or a list of --replace KEY=LOCAL flags. The returned baseDir is
// where any source: paths in the patches will be resolved against
// (caller passes through to BuildFilePatchActions). For --replace,
// each LOCAL is converted to an absolute path so baseDir is irrelevant.
func loadPatches(replaces []string, patchesPath string) ([]bundle.FilePatch, string, error) {
	if patchesPath != "" {
		raw, err := os.ReadFile(patchesPath)
		if err != nil {
			return nil, "", fmt.Errorf("reading --patches file: %w", err)
		}
		var pf patchesFile
		if err := yaml.Unmarshal(raw, &pf); err != nil {
			return nil, "", fmt.Errorf("parsing --patches file: %w", err)
		}
		// schema_version is optional but if set must be 1; rejecting
		// future schemas keeps this tool honest about what it understands.
		if pf.SchemaVersion != 0 && pf.SchemaVersion != 1 {
			return nil, "", fmt.Errorf("unsupported patches schema_version %d (expected 1)", pf.SchemaVersion)
		}
		baseDir, err := filepath.Abs(filepath.Dir(patchesPath))
		if err != nil {
			return nil, "", err
		}
		return pf.Patches, baseDir, nil
	}

	patches := make([]bundle.FilePatch, 0, len(replaces))
	for _, raw := range replaces {
		eq := strings.IndexByte(raw, '=')
		if eq <= 0 || eq == len(raw)-1 {
			return nil, "", fmt.Errorf("--replace %q must be of the form <onramp-rel-path>=<local-file>", raw)
		}
		target := raw[:eq]
		local := raw[eq+1:]
		abs, err := filepath.Abs(local)
		if err != nil {
			return nil, "", fmt.Errorf("--replace %q: resolving local path: %w", raw, err)
		}
		patches = append(patches, bundle.FilePatch{
			Target: target,
			Source: abs,
		})
	}
	return patches, "", nil
}

func shortHash(s string) string {
	if len(s) < 12 {
		return s
	}
	return s[:12]
}
