package builder

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
	"github.com/aether-gui/aether-ops-bootstrap/internal/deb"
)

// sections to search for packages.
var indexSections = []string{"main", "universe"}

type resolvedAptSource struct {
	Name          string
	URL           string
	Components    []string
	Suites        []string
	Architectures []string
}

// FetchDebs resolves transitive dependencies for the requested packages
// and downloads all .deb files for each (suite, arch) pair in the spec.
func FetchDebs(ctx context.Context, dl *Downloader, spec *bundle.Spec, stageDir string) ([]bundle.DebEntry, error) {
	if len(spec.Debs) == 0 {
		return nil, nil
	}

	// Build the list of wanted package names and constraints.
	var wanted []string
	constraints := make(map[string]deb.Constraint)
	for _, d := range spec.Debs {
		wanted = append(wanted, d.Name)
		if d.Version != "" {
			c, err := deb.ParseConstraint(d.Version)
			if err != nil {
				return nil, fmt.Errorf("parsing version constraint for %s: %w", d.Name, err)
			}
			constraints[d.Name] = c
		}
	}

	var allEntries []bundle.DebEntry
	sources := aptSourcesForSpec(spec)

	for _, suite := range spec.Ubuntu.Suites {
		for _, arch := range spec.Ubuntu.Architectures {
			log.Printf("resolving .deb dependencies for %s/%s", suite, arch)

			// Fetch and parse Packages indexes.
			idx, err := fetchPackageIndex(ctx, dl, sources, suite, arch)
			if err != nil {
				return nil, fmt.Errorf("fetching package index for %s/%s: %w", suite, arch, err)
			}

			// Resolve dependencies.
			resolved, err := deb.Resolve(wanted, idx, constraints)
			if err != nil {
				return nil, fmt.Errorf("resolving dependencies for %s/%s: %w", suite, arch, err)
			}
			log.Printf("resolved %d packages for %s/%s", len(resolved), suite, arch)

			// Download each .deb.
			debDir := filepath.Join(stageDir, "debs", suite, arch)
			if err := os.MkdirAll(debDir, 0755); err != nil {
				return nil, err
			}

			for _, pkg := range resolved {
				url := fmt.Sprintf("%s/%s", pkg.SourceURL, pkg.Filename)
				basename := filepath.Base(pkg.Filename)
				destPath := filepath.Join(debDir, basename)

				if pkg.SHA256 == "" {
					return nil, fmt.Errorf("missing SHA256 for package %s %s in %s/%s", pkg.Name, pkg.Version, suite, arch)
				}

				if _, err := dl.Download(ctx, url, destPath); err != nil {
					return nil, fmt.Errorf("downloading %s: %w", basename, err)
				}

				if err := VerifyArtifact(destPath, pkg.SHA256); err != nil {
					return nil, err
				}

				allEntries = append(allEntries, bundle.DebEntry{
					Name:     pkg.Name,
					Version:  pkg.Version,
					Arch:     pkg.Arch,
					Suite:    suite,
					Filename: filepath.Join("debs", suite, arch, basename),
					SHA256:   pkg.SHA256,
				})
			}
		}
	}

	return allEntries, nil
}

func aptSourcesForSpec(spec *bundle.Spec) []resolvedAptSource {
	sources := []resolvedAptSource{
		{
			Name:          "ubuntu",
			URL:           spec.Ubuntu.Mirror,
			Components:    indexSections,
			Suites:        spec.Ubuntu.Suites,
			Architectures: spec.Ubuntu.Architectures,
		},
	}
	for _, src := range spec.AptSources {
		suites := src.Suites
		if len(suites) == 0 {
			suites = spec.Ubuntu.Suites
		}
		arches := src.Architectures
		if len(arches) == 0 {
			arches = spec.Ubuntu.Architectures
		}
		sources = append(sources, resolvedAptSource{
			Name:          src.Name,
			URL:           strings.TrimRight(src.URL, "/"),
			Components:    src.Components,
			Suites:        suites,
			Architectures: arches,
		})
	}
	return sources
}

// fetchPackageIndex downloads and parses Packages.gz from all sections
// for the given suite and architecture across every configured source, merging the
// results into a single index. Also fetches binary-all indexes for
// architecture-independent packages.
func fetchPackageIndex(ctx context.Context, dl *Downloader, sources []resolvedAptSource, suite, arch string) (*deb.Index, error) {
	var allPkgs []deb.Package

	arches := []string{arch, "all"}
	for _, src := range sources {
		if !containsString(src.Suites, suite) || !containsString(src.Architectures, arch) {
			continue
		}
		for _, component := range src.Components {
			for _, a := range arches {
				url := fmt.Sprintf("%s/dists/%s/%s/binary-%s/Packages.gz",
					src.URL, suite, component, a)

				tmpFile, err := os.CreateTemp("", "packages-*.gz")
				if err != nil {
					return nil, err
				}
				tmpPath := tmpFile.Name()
				tmpFile.Close()
				defer os.Remove(tmpPath)

				if _, err := dl.Download(ctx, url, tmpPath); err != nil {
					var httpErr *HTTPError
					if errors.As(err, &httpErr) && httpErr.StatusCode == 404 {
						log.Printf("skipping %s:%s/%s/binary-%s (not available)", src.Name, suite, component, a)
						continue
					}
					return nil, fmt.Errorf("downloading %s:%s/%s/binary-%s/Packages.gz: %w", src.Name, suite, component, a, err)
				}

				pkgs, err := parsePackagesGz(tmpPath)
				if err != nil {
					return nil, fmt.Errorf("parsing %s:%s/%s/binary-%s/Packages.gz: %w", src.Name, suite, component, a, err)
				}
				for i := range pkgs {
					pkgs[i].SourceName = src.Name
					pkgs[i].SourceURL = src.URL
				}
				allPkgs = append(allPkgs, pkgs...)
			}
		}
	}

	if len(allPkgs) == 0 {
		return nil, fmt.Errorf("no packages found for %s/%s", suite, arch)
	}

	return deb.NewIndex(allPkgs), nil
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

// parsePackagesGz decompresses and parses a Packages.gz file.
func parsePackagesGz(path string) ([]deb.Package, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("decompressing %s: %w", path, err)
	}
	defer gz.Close()

	return deb.ParseControl(gz)
}
