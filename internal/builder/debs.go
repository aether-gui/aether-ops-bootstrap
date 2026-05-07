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

// DebsResult bundles everything FetchDebs produces. The on-host
// installer needs the manifest entries; the apt-repo builder needs
// the parsed packages (for their RawStanza); the launcher's
// sources.list needs the codenames that ended up in the bundle.
type DebsResult struct {
	Entries   []bundle.DebEntry
	Packages  []*deb.Package
	Codenames []string
}

// FetchDebs resolves transitive dependencies for the requested packages,
// downloads all .deb files into apt-repo/pool/<codename>/<arch>/, and
// emits the dists/<codename>/* metadata that turns the staged tree into
// a real file:// apt repository. Apt suites are grouped by their
// release codename (the release pocket plus its -updates / -security
// pockets are merged into a single index per arch), so the resolver
// picks the highest available version across pockets — the same
// behaviour `apt install` gives operators on a fresh Ubuntu host.
// Bundle entries record the base release codename in Suite, regardless
// of which pocket the chosen version came from, so the on-host
// installer's suite filter keeps matching.
func FetchDebs(ctx context.Context, dl *Downloader, spec *bundle.Spec, stageDir string) (*DebsResult, error) {
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
	var allResolved []*deb.Package
	var codenames []string
	sources := aptSourcesForSpec(spec)

	groups := groupSuitesByBase(spec.Ubuntu.Suites)
	for _, group := range groups {
		baseSuite := group.base
		codenames = append(codenames, baseSuite)
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

			// Stage each .deb under apt-repo/pool/<codename>/<pkg-arch>/.
			// The on-host installer's apt-get points at the apt-repo/
			// root via a sources.list entry; pool/ is the standard
			// Debian archive layout for the actual .deb files. The
			// resolved set contains both the requested arch and arch=all
			// packages — each one lands in the directory matching its
			// own Architecture: field so apt-repo's binary-amd64 and
			// binary-all metadata can reference them correctly.
			for _, pkg := range resolved {
				url := fmt.Sprintf("%s/%s", pkg.SourceURL, pkg.Filename)
				basename := filepath.Base(pkg.Filename)
				poolDir := filepath.Join(stageDir, "apt-repo", "pool", baseSuite, pkg.Arch)
				if err := os.MkdirAll(poolDir, 0755); err != nil {
					return nil, err
				}
				destPath := filepath.Join(poolDir, basename)

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
					Filename: filepath.ToSlash(filepath.Join("apt-repo", "pool", baseSuite, pkg.Arch, basename)),
					SHA256:   pkg.SHA256,
				})
				allResolved = append(allResolved, pkg)
			}
		}
	}

	// Generate dists/<codename>/* metadata so the staged tree is a
	// real apt repository the launcher can hand to apt-get install.
	if err := BuildAptRepo(stageDir, allResolved, codenames); err != nil {
		return nil, fmt.Errorf("building apt repo metadata: %w", err)
	}

	return &DebsResult{
		Entries:   allEntries,
		Packages:  allResolved,
		Codenames: codenames,
	}, nil
}

// BaseSuite strips a pocket suffix to yield the release codename
// (e.g. "noble-updates" → "noble"). Already-bare codenames pass
// through unchanged. Exported so cmd/build-bundle can compute the
// distinct codename set for the manifest's AptRepoEntry.
func BaseSuite(s string) string {
	return baseSuite(s)
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
