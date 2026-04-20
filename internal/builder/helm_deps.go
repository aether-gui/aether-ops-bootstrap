package builder

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// chartMeta is the subset of Chart.yaml this builder cares about. Only
// the dependencies list is inspected — other fields are ignored.
type chartMeta struct {
	Dependencies []chartDep `yaml:"dependencies"`
}

type chartDep struct {
	Name       string `yaml:"name"`
	Version    string `yaml:"version"`
	Repository string `yaml:"repository"`
}

// resolveChartDependencies walks root for Chart.yaml files whose
// `dependencies:` include at least one non-local (http/oci) repository
// and runs `helm dep up` on each such chart so the resulting
// charts/*.tgz is on disk before the tree is hashed into the manifest.
//
// Charts with only file:// dependencies are skipped — those resolve
// at install time from sibling charts already shipped in the bundle.
// Charts with no dependencies at all are skipped too.
//
// If helmBinary is empty, dep resolution is skipped entirely. That
// keeps test fixtures (which rarely need it) simple; production
// callers pass the Helm binary staged by FetchHelm.
func resolveChartDependencies(ctx context.Context, helmBinary, root string) error {
	if helmBinary == "" {
		return nil
	}

	charts, err := findChartsNeedingDeps(root)
	if err != nil {
		return fmt.Errorf("scanning chart tree: %w", err)
	}

	for _, chartDir := range charts {
		log.Printf("  helm dep up %s", chartDir)
		cmd := exec.CommandContext(ctx, helmBinary, "dep", "up", chartDir)
		cmd.Env = append(os.Environ(),
			// Isolate the build's helm state from the user's
			// ~/.cache and ~/.config so parallel builds don't
			// stomp each other.
			"HELM_CACHE_HOME="+filepath.Join(root, ".helm-cache"),
			"HELM_CONFIG_HOME="+filepath.Join(root, ".helm-config"),
			"HELM_DATA_HOME="+filepath.Join(root, ".helm-data"),
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("helm dep up %s: %w\n%s", chartDir, err, out)
		}
	}

	// Leave no build-time helm state behind in the hashed tree.
	for _, dir := range []string{".helm-cache", ".helm-config", ".helm-data"} {
		_ = os.RemoveAll(filepath.Join(root, dir))
	}

	return nil
}

// findChartsNeedingDeps returns directories containing a Chart.yaml
// with at least one non-file:// dependency. Deep ordering is
// preserved so later callers can resolve sub-charts before their
// parents if needed.
func findChartsNeedingDeps(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != "Chart.yaml" {
			return nil
		}

		raw, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}
		var meta chartMeta
		if err := yaml.Unmarshal(raw, &meta); err != nil {
			// Not every Chart.yaml we might encounter is
			// well-formed for our narrow subset — ignore parse
			// errors rather than fail the whole build.
			return nil
		}
		if !hasRemoteDependency(meta.Dependencies) {
			return nil
		}
		out = append(out, filepath.Dir(path))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// hasRemoteDependency reports whether any dependency's repository
// resolves to a remote registry (http/https/oci). file:// entries are
// handled by helm at install time from sibling charts in the bundle.
func hasRemoteDependency(deps []chartDep) bool {
	for _, d := range deps {
		repo := strings.ToLower(strings.TrimSpace(d.Repository))
		if strings.HasPrefix(repo, "http://") ||
			strings.HasPrefix(repo, "https://") ||
			strings.HasPrefix(repo, "oci://") {
			return true
		}
	}
	return false
}
