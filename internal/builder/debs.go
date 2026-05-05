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
// and downloads all .deb files. Apt suites are grouped by their release
// codename (the release pocket plus its -updates / -security pockets are
// merged into a single index per arch), so the resolver picks the highest
// available version across pockets — the same behaviour `apt install`
// gives operators on a fresh Ubuntu host. Bundle entries record the base
// release codename in Suite, regardless of which pocket the chosen
// version came from, so the on-host installer's suite filter keeps
// matching.
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

	groups := groupSuitesByBase(spec.Ubuntu.Suites)
	for _, group := range groups {
		baseSuite := group.base
		for _, arch := range spec.Ubuntu.Architectures {
			log.Printf("resolving .deb dependencies for %s/%s (pockets: %v)", baseSuite, arch, group.suites)

			// Aggregate Packages indexes across every pocket in the
			// group (release + updates + security). NewIndex keeps
			// the highest version per package, which is what apt
			// would pick on a vanilla host.
			var allPkgs []deb.Package
			for _, suite := range group.suites {
				pkgs, err := fetchPackagesForSuite(ctx, dl, sources, suite, arch)
				if err != nil {
					return nil, fmt.Errorf("fetching package index for %s/%s: %w", suite, arch, err)
				}
				allPkgs = append(allPkgs, pkgs...)
			}
			if len(allPkgs) == 0 {
				return nil, fmt.Errorf("no packages found for %s/%s across pockets %v", baseSuite, arch, group.suites)
			}

			idx := deb.NewIndex(allPkgs)
			resolved, err := deb.Resolve(wanted, idx, constraints)
			if err != nil {
				return nil, fmt.Errorf("resolving dependencies for %s/%s: %w", baseSuite, arch, err)
			}
			log.Printf("resolved %d packages for %s/%s", len(resolved), baseSuite, arch)

			// Download each .deb. Stage under the base suite so the
			// on-host installer's suite filter (which compares
			// against the host's release codename, not the pocket)
			// continues to match.
			debDir := filepath.Join(stageDir, "debs", baseSuite, arch)
			if err := os.MkdirAll(debDir, 0755); err != nil {
				return nil, err
			}

			for _, pkg := range resolved {
				url := fmt.Sprintf("%s/%s", pkg.SourceURL, pkg.Filename)
				basename := filepath.Base(pkg.Filename)
				destPath := filepath.Join(debDir, basename)

				if pkg.SHA256 == "" {
					return nil, fmt.Errorf("missing SHA256 for package %s %s in %s/%s", pkg.Name, pkg.Version, baseSuite, arch)
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
					Suite:    baseSuite,
					Filename: filepath.Join("debs", baseSuite, arch, basename),
					SHA256:   pkg.SHA256,
				})
			}
		}
	}

	return allEntries, nil
}

// suiteGroup is one release codename plus the pockets configured for
// it (release / -updates / -security / -backports / -proposed).
type suiteGroup struct {
	base   string
	suites []string
}

// pocketSuffixes are the standard apt pockets that share an index
// with their release codename.
var pocketSuffixes = []string{"-updates", "-security", "-backports", "-proposed"}

// baseSuite strips a pocket suffix to yield the release codename.
// Already-bare codenames pass through unchanged.
func baseSuite(s string) string {
	for _, suffix := range pocketSuffixes {
		if strings.HasSuffix(s, suffix) {
			return strings.TrimSuffix(s, suffix)
		}
	}
	return s
}

// groupSuitesByBase keeps the order of first appearance of each base
// codename in suites so build output is deterministic and matches the
// spec's ordering.
func groupSuitesByBase(suites []string) []suiteGroup {
	idx := make(map[string]int)
	var groups []suiteGroup
	for _, s := range suites {
		base := baseSuite(s)
		if i, ok := idx[base]; ok {
			groups[i].suites = append(groups[i].suites, s)
			continue
		}
		idx[base] = len(groups)
		groups = append(groups, suiteGroup{base: base, suites: []string{s}})
	}
	return groups
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

// fetchPackagesForSuite downloads and parses Packages.gz from every
// configured source for the given suite and architecture, returning
// the raw package entries tagged with the suite they came from.
// Callers compose these across pockets and build a single index.
//
// Sources whose advertised Suites/Architectures don't include the
// requested pair are skipped silently. A 404 on an individual
// (component, arch) is also skipped (third-party repos like Docker
// publish only `<release>/stable/binary-amd64`, no `binary-all`,
// no `noble-updates/`, etc.); other download errors are fatal.
func fetchPackagesForSuite(ctx context.Context, dl *Downloader, sources []resolvedAptSource, suite, arch string) ([]deb.Package, error) {
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
					pkgs[i].Suite = suite
				}
				allPkgs = append(allPkgs, pkgs...)
			}
		}
	}

	return allPkgs, nil
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
