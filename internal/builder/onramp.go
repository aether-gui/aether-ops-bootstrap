package builder

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
)

// BuildOnramp clones the aether-onramp repository into the bundle staging
// area so it can be shipped for airgapped installs. The cloned tree is
// placed at "onramp/aether-onramp" relative to stageDir; the manifest
// records that path plus the resolved commit SHA for reproducibility.
func BuildOnramp(ctx context.Context, spec *bundle.OnrampSpec, stageDir string) (*bundle.OnrampEntry, error) {
	relPath := filepath.Join("onramp", "aether-onramp")
	destDir := filepath.Join(stageDir, relPath)

	resolvedSHA, err := cloneRepo(ctx, spec.Repo, spec.Ref, destDir, spec.RecurseSubmodules)
	if err != nil {
		return nil, fmt.Errorf("cloning onramp: %w", err)
	}

	files, err := hashTree(destDir, relPath)
	if err != nil {
		return nil, fmt.Errorf("hashing onramp tree: %w", err)
	}

	return &bundle.OnrampEntry{
		Repo:        spec.Repo,
		Ref:         spec.Ref,
		ResolvedSHA: resolvedSHA,
		Path:        relPath,
		Files:       files,
	}, nil
}

// BuildHelmCharts clones each configured helm chart repo into the staging
// area and returns manifest entries describing them. Each repo is placed
// under "helm-charts/<name>". Submodules are not recursed by default —
// chart repos rarely need them, and enabling them risks bloating the bundle.
func BuildHelmCharts(ctx context.Context, specs []bundle.HelmChartsSpec, stageDir string) ([]bundle.HelmChartsEntry, error) {
	var out []bundle.HelmChartsEntry
	for _, hc := range specs {
		relPath := filepath.Join("helm-charts", hc.Name)
		destDir := filepath.Join(stageDir, relPath)

		resolvedSHA, err := cloneRepo(ctx, hc.Repo, hc.Ref, destDir, false)
		if err != nil {
			return nil, fmt.Errorf("cloning helm chart %q: %w", hc.Name, err)
		}

		files, err := hashTree(destDir, relPath)
		if err != nil {
			return nil, fmt.Errorf("hashing helm chart %q tree: %w", hc.Name, err)
		}

		out = append(out, bundle.HelmChartsEntry{
			Name:        hc.Name,
			Repo:        hc.Repo,
			Ref:         hc.Ref,
			ResolvedSHA: resolvedSHA,
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

// hashTree walks a directory and returns a BundleFile entry for every
// regular file found. The .git directory is skipped because it is large,
// ephemeral, and has no value in the installed bundle.
//
// bundleRelRoot is the directory path the entries should be recorded as,
// relative to the bundle root (e.g. "onramp/aether-onramp"). File paths
// in the returned entries are bundle-relative.
func hashTree(rootDir, bundleRelRoot string) ([]bundle.BundleFile, error) {
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
	return files, nil
}
