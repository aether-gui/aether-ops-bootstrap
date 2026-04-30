package builder

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aether-gui/aether-ops-bootstrap/internal/builder/patch"
	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
)

// localSDCoreChartDir is the on-target path of the sd-core umbrella
// chart after the launcher extracts the bundled helm-charts repo.
// The onramp 5gc core role derives its source root as the dirname of
// chart_ref and its install dir as the basename, so pointing at the
// umbrella sub-directory makes `synchronize` copy every sibling
// component chart too (file:// deps resolve inside /tmp/sdcore-helm-charts).
const localSDCoreChartDir = "/var/lib/aether-ops/helm-charts/sdcore-helm-charts/sdcore-helm-charts"

// onrampPatches returns the mutations applied to every vendored
// aether-onramp checkout after clone, adapting upstream's online-first
// defaults to this bootstrap's offline deployment model.
func onrampPatches() []patch.Action {
	return []patch.Action{
		// Upstream defaults `airgapped.enabled: false` to keep stock
		// online installs unchanged. Bundles built here always target
		// offline hosts, so the flag must be on before playbooks run —
		// otherwise `apt: update_cache=yes` tasks hit unreachable
		// archives. See opennetworkinglab/aether-onramp#180.
		patch.SetYAMLField{
			RelPath: "vars/main.yml",
			KeyPath: []string{"airgapped", "enabled"},
			Value:   true,
		},
		// Upstream defaults sd-core's chart_ref to an OCI registry
		// (`oci://ghcr.io/omec-project/sd-core`) with local_charts off.
		// In airgap ghcr.io is unreachable; flip to local_charts and
		// point at the umbrella chart dir shipped in the bundle.
		patch.SetYAMLField{
			RelPath: "vars/main.yml",
			KeyPath: []string{"core", "helm", "local_charts"},
			Value:   true,
		},
		patch.SetYAMLField{
			RelPath: "vars/main.yml",
			KeyPath: []string{"core", "helm", "chart_ref"},
			Value:   localSDCoreChartDir,
		},
	}
}

// BuildOnramp clones the aether-onramp repository into the bundle staging
// area so it can be shipped for airgapped installs. The cloned tree is
// placed at "onramp/aether-onramp" relative to stageDir; the manifest
// records that path plus the resolved commit SHA for reproducibility.
//
// specDir is the directory of the bundle spec file; it is used to
// resolve any relative `source:` paths in spec.Patches. Pass "" when
// no user patches are configured.
func BuildOnramp(ctx context.Context, spec *bundle.OnrampSpec, stageDir, specDir string) (*bundle.OnrampEntry, error) {
	relPath := filepath.Join("onramp", "aether-onramp")
	destDir := filepath.Join(stageDir, relPath)

	resolvedSHA, err := cloneRepo(ctx, spec.Repo, spec.Ref, destDir, spec.RecurseSubmodules)
	if err != nil {
		return nil, fmt.Errorf("cloning onramp: %w", err)
	}

	// Apply onramp-specific patches to the cloned tree before
	// hashing so the manifest reflects the on-disk state shipped.
	for _, action := range onrampPatches() {
		if err := action.Apply(destDir); err != nil {
			return nil, fmt.Errorf("patching onramp: %s: %w", action.Name(), err)
		}
		log.Printf("  onramp patch: %s", action.Name())
	}

	// User patches run after the built-in adaptations so they can
	// override anything the build does (and so a user `replace_file`
	// on `vars/main.yml` would clobber the airgapped flag if intended).
	userActions, err := BuildFilePatchActions(spec.Patches, specDir)
	if err != nil {
		return nil, fmt.Errorf("resolving onramp.patches: %w", err)
	}
	for _, action := range userActions {
		if err := action.Apply(destDir); err != nil {
			return nil, fmt.Errorf("applying onramp.patches: %s: %w", action.Name(), err)
		}
		log.Printf("  onramp patch: %s", action.Name())
	}

	files, err := HashTree(destDir, relPath)
	if err != nil {
		return nil, fmt.Errorf("hashing onramp tree: %w", err)
	}

	return &bundle.OnrampEntry{
		Repo:        spec.Repo,
		Ref:         spec.Ref,
		ResolvedSHA: resolvedSHA,
		TreeSHA256:  bundle.ComputeTreeSHA256(files),
		Path:        relPath,
		Files:       files,
	}, nil
}

// BuildFilePatchActions resolves the source files and inline content
// in a FilePatch slice into a sequence of patch.Action values ready to
// apply against a cloned tree. baseDir is used to resolve any
// relative source paths; pass "" when patches is empty or only uses
// inline content.
func BuildFilePatchActions(patches []bundle.FilePatch, baseDir string) ([]patch.Action, error) {
	if len(patches) == 0 {
		return nil, nil
	}
	out := make([]patch.Action, 0, len(patches))
	for i, p := range patches {
		body, err := resolveFilePatchBody(p, baseDir)
		if err != nil {
			return nil, fmt.Errorf("patches[%d] (%s): %w", i, p.Target, err)
		}
		action := patch.ReplaceFile{
			RelPath: filepath.FromSlash(p.Target),
			Body:    body,
		}
		if p.FileMode != nil {
			mode := os.FileMode(*p.FileMode)
			action.Mode = &mode
		}
		out = append(out, action)
	}
	return out, nil
}

func resolveFilePatchBody(p bundle.FilePatch, baseDir string) ([]byte, error) {
	if p.Source != "" {
		full := p.Source
		if !filepath.IsAbs(full) {
			if baseDir == "" {
				return nil, fmt.Errorf("source %q is relative but no base directory was provided", p.Source)
			}
			full = filepath.Join(baseDir, p.Source)
		}
		body, err := os.ReadFile(full)
		if err != nil {
			return nil, fmt.Errorf("reading source: %w", err)
		}
		return body, nil
	}
	return []byte(p.Content), nil
}

// BuildHelmCharts clones each configured helm chart repo into the staging
// area and returns manifest entries describing them. Each repo is placed
// under "helm-charts/<name>". Submodules are not recursed by default —
// chart repos rarely need them, and enabling them risks bloating the bundle.
//
// helmBinary, if non-empty, is used to resolve chart dependencies with
// `helm dep up` after clone so the resulting charts/*.tgz ship in the
// bundle; airgapped installs cannot reach the upstream chart repos to
// do this themselves. Callers that don't need dep resolution (most
// tests) may pass "".
func BuildHelmCharts(ctx context.Context, specs []bundle.HelmChartsSpec, stageDir, helmBinary string) ([]bundle.HelmChartsEntry, error) {
	var out []bundle.HelmChartsEntry
	for _, hc := range specs {
		relPath := filepath.Join("helm-charts", hc.Name)
		destDir := filepath.Join(stageDir, relPath)

		resolvedSHA, err := cloneRepo(ctx, hc.Repo, hc.Ref, destDir, false)
		if err != nil {
			return nil, fmt.Errorf("cloning helm chart %q: %w", hc.Name, err)
		}

		if err := resolveChartDependencies(ctx, helmBinary, destDir); err != nil {
			return nil, fmt.Errorf("resolving helm deps for %q: %w", hc.Name, err)
		}

		files, err := HashTree(destDir, relPath)
		if err != nil {
			return nil, fmt.Errorf("hashing helm chart %q tree: %w", hc.Name, err)
		}

		out = append(out, bundle.HelmChartsEntry{
			Name:        hc.Name,
			Repo:        hc.Repo,
			Ref:         hc.Ref,
			ResolvedSHA: resolvedSHA,
			TreeSHA256:  bundle.ComputeTreeSHA256(files),
			Path:        relPath,
			Files:       files,
		})
	}
	return out, nil
}

// cloneRepo clones a git repository into destDir, optionally with
// submodules. When ref is empty the remote HEAD is used. Returns the
// resolved commit SHA so the manifest can pin reproducibility even when
// the source ref is a moving branch.
//
// The clone is full-history (not shallow) because shallow clones of
// arbitrary refs can fail and submodule traversal on shallow clones is
// fragile. The resulting working tree is left intact — the caller may
// remove `.git` after hashing if a smaller bundle is preferred.
func cloneRepo(ctx context.Context, url, ref, destDir string, recurse bool) (string, error) {
	if err := os.RemoveAll(destDir); err != nil {
		return "", fmt.Errorf("cleaning clone destination: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(destDir), 0755); err != nil {
		return "", fmt.Errorf("creating parent dir: %w", err)
	}

	args := []string{"clone"}
	if recurse {
		args = append(args, "--recurse-submodules")
	}
	args = append(args, url, destDir)
	log.Printf("cloning %s", url)
	if err := execCmd(ctx, "", "git", args...); err != nil {
		return "", err
	}

	// Check out the requested ref.
	if ref != "" {
		if err := execCmd(ctx, destDir, "git", "checkout", ref); err != nil {
			return "", fmt.Errorf("checkout %s: %w", ref, err)
		}
		if recurse {
			if err := execCmd(ctx, destDir, "git", "submodule", "update", "--init", "--recursive"); err != nil {
				return "", fmt.Errorf("updating submodules after checkout: %w", err)
			}
		}
	}

	out, err := execCmdOutput(ctx, destDir, "git", "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("resolving HEAD: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// HashTree walks a directory and returns a BundleFile entry for every
// regular file found. The .git directory is skipped because it is large,
// ephemeral, and has no value in the installed bundle. Entries are
// returned sorted by Path so manifests built from the same tree are
// byte-stable across runs.
//
// bundleRelRoot is the directory path the entries should be recorded as,
// relative to the bundle root (e.g. "onramp/aether-onramp"). File paths
// in the returned entries are bundle-relative.
func HashTree(rootDir, bundleRelRoot string) ([]bundle.BundleFile, error) {
	var files []bundle.BundleFile
	err := filepath.WalkDir(rootDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		// Only hash regular files (skip symlinks and special files).
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}

		rel, err := filepath.Rel(rootDir, path)
		if err != nil {
			return err
		}
		var hash string
		if err := computeFileSHA256(path, &hash); err != nil {
			return err
		}
		files = append(files, bundle.BundleFile{
			Path:   filepath.Join(bundleRelRoot, rel),
			SHA256: hash,
			Size:   info.Size(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}
