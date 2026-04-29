package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aether-gui/aether-ops-bootstrap/internal/builder"
	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
	"github.com/aether-gui/aether-ops-bootstrap/internal/deb"
)

var gitSHA string // set via ldflags: -X main.gitSHA=...

func main() {
	specPath := flag.String("spec", "specs/bundle.yaml", "path to spec file or directory of spec files")
	output := flag.String("output", "dist/bundle.tar.zst", "output path (file for single spec, directory for multi-spec)")
	flag.Parse()

	info, err := os.Stat(*specPath)
	if err != nil {
		log.Fatalf("stat %s: %v", *specPath, err)
	}

	if !info.IsDir() {
		// Single spec mode.
		lockPath := strings.TrimSuffix(*specPath, filepath.Ext(*specPath)) + ".lock.json"
		if err := buildOne(*specPath, *output, lockPath); err != nil {
			log.Fatalf("build failed: %v", err)
		}
		return
	}

	// Multi-spec mode: iterate over *.yaml files in the directory.
	// When --spec is a directory, --output must be a directory too.
	// If it looks like a file path (has an extension), use its parent.
	entries, err := os.ReadDir(*specPath)
	if err != nil {
		log.Fatalf("reading spec directory: %v", err)
	}

	outputDir := *output
	if filepath.Ext(outputDir) != "" {
		outputDir = filepath.Dir(outputDir)
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		log.Fatalf("creating output directory: %v", err)
	}

	var built int
	for _, e := range entries {
		if e.IsDir() || (!strings.HasSuffix(e.Name(), ".yaml") && !strings.HasSuffix(e.Name(), ".yml")) {
			continue
		}
		specFile := filepath.Join(*specPath, e.Name())
		baseName := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		outFile := filepath.Join(outputDir, baseName+".tar.zst")
		lockFile := filepath.Join(*specPath, baseName+".lock.json")

		log.Printf("=== building %s ===", e.Name())
		if err := buildOne(specFile, outFile, lockFile); err != nil {
			log.Fatalf("build %s failed: %v", e.Name(), err)
		}
		built++
	}

	if built == 0 {
		log.Fatalf("no .yaml spec files found in %s", *specPath)
	}
	log.Printf("built %d bundles", built)
}

func buildOne(specPath, outputPath, lockPath string) error {
	// Parse and validate spec.
	spec, err := bundle.ParseSpec(specPath)
	if err != nil {
		return err
	}
	if err := bundle.ValidateSpec(spec); err != nil {
		return err
	}

	// Create temp staging directory.
	stageDir, err := os.MkdirTemp("", "aether-bundle-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stageDir)

	ctx := context.Background()
	dl := &builder.Downloader{Client: &http.Client{Timeout: 30 * time.Minute}}

	// Build RKE2 artifacts.
	var rke2Entry *bundle.RKE2Entry
	if spec.RKE2 != nil {
		log.Printf("fetching RKE2 %s artifacts...", spec.RKE2.Version)
		rke2Entry, err = builder.FetchAndVerifyRKE2(ctx, dl, spec.RKE2, spec.Ubuntu.Architectures, stageDir)
		if err != nil {
			return err
		}
		log.Printf("RKE2 artifacts staged (%d files)", len(rke2Entry.Artifacts))
	}

	// Fetch Helm binary.
	var helmEntry *bundle.HelmEntry
	if spec.Helm != nil {
		log.Printf("fetching Helm %s...", spec.Helm.Version)
		helmEntry, err = builder.FetchHelm(ctx, dl, spec.Helm, stageDir)
		if err != nil {
			return err
		}
		log.Printf("Helm staged (%d files)", len(helmEntry.Files))
	}

	// Build aether-ops artifacts.
	var aetherOpsEntry *bundle.AetherOpsEntry
	if spec.AetherOps != nil {
		log.Printf("building aether-ops %s...", spec.AetherOps.Version)
		aetherOpsEntry, err = builder.BuildAetherOps(ctx, dl, spec.AetherOps, stageDir)
		if err != nil {
			return err
		}
		log.Printf("aether-ops staged (%d files)", len(aetherOpsEntry.Files))
	}

	// Clone aether-onramp early so downstream dependency discovery can
	// feed later artifact resolution (debs, wheels, validation).
	var onrampEntry *bundle.OnrampEntry
	var onrampScan *builder.OnrampDependencyScan
	if spec.Onramp != nil {
		log.Printf("cloning aether-onramp from %s...", spec.Onramp.Repo)
		onrampEntry, err = builder.BuildOnramp(ctx, spec.Onramp, stageDir)
		if err != nil {
			return err
		}
		log.Printf("onramp staged at %s (sha %s, %d files)", onrampEntry.Path, onrampEntry.ResolvedSHA[:min(12, len(onrampEntry.ResolvedSHA))], len(onrampEntry.Files))

		onrampRoot := filepath.Join(stageDir, onrampEntry.Path)
		onrampScan, err = builder.ScanOnrampDependencies(onrampRoot)
		if err != nil {
			return fmt.Errorf("scanning onramp dependencies: %w", err)
		}
		log.Printf("onramp scan discovered %d apt packages and %d pip requirements", len(onrampScan.AptPackages), len(onrampScan.PipRequirements))
		if len(onrampScan.Unresolved) > 0 {
			return fmt.Errorf(
				"onramp dependency scan found %d unresolved offline requirement(s).\n"+
					"Bundle build continued far enough to collect everything the scanner could resolve, "+
					"but these references still need to be addressed before offline bundling is reliable.\n\n"+
					"Unresolved references:\n- %s",
				len(onrampScan.Unresolved),
				strings.Join(onrampScan.Unresolved, "\n- "),
			)
		}
	}

	effectiveAptSources, discoveredAptSources := mergeAptSources(spec.AptSources, onrampScan)

	// Fetch .deb packages.
	var debEntries []bundle.DebEntry
	effectiveDebs, discoveredDebs := mergeDebSpecs(spec.Debs, onrampScan)
	if len(effectiveDebs) > 0 {
		log.Printf("resolving and fetching .deb packages...")
		specWithDiscoveredDebs := *spec
		specWithDiscoveredDebs.Debs = effectiveDebs
		specWithDiscoveredDebs.AptSources = effectiveAptSources
		debEntries, err = builder.FetchDebs(ctx, dl, &specWithDiscoveredDebs, stageDir)
		if err != nil {
			var missingErr *deb.MissingPackagesError
			if errors.As(err, &missingErr) {
				return formatMissingDebsError(spec.Ubuntu.Suites, effectiveDebs, discoveredDebs, discoveredAptSources, onrampScan, missingErr.Names)
			}
			return err
		}
		log.Printf("staged %d .deb packages", len(debEntries))

		// Lockfile: build current, verify against existing, write updated.
		currentLock := builder.BuildLockfile(debEntries)
		existingLock, err := builder.ReadLockfile(lockPath)
		if err != nil {
			return err
		}
		if existingLock != nil {
			if err := builder.VerifyLockfile(existingLock, currentLock); err != nil {
				log.Printf("WARNING: %v", err)
			}
		}
		if err := builder.WriteLockfile(lockPath, currentLock); err != nil {
			return err
		}
	}

	// Build an offline wheelhouse for pip requirements referenced by the
	// vendored onramp tree.
	var wheelhouseEntry *bundle.WheelhouseEntry
	if onrampScan != nil && len(onrampScan.PipRequirements) > 0 {
		wheelPlan := builder.PlanWheelhouseRequirements(onrampScan.PipRequirements, effectiveDebs)
		for _, advisory := range wheelPlan.DistroSatisfiedPip {
			log.Printf("wheelhouse advisory: using bundled distro package for %s", advisory)
		}
		if len(wheelPlan.Requirements) > 0 {
			log.Printf("building wheelhouse for %d pip requirements...", len(wheelPlan.Requirements))
			wheelhouseEntry, err = builder.BuildWheelhouse(ctx, wheelPlan.Requirements, stageDir)
			if err != nil {
				return err
			}
			if wheelhouseEntry != nil {
				log.Printf("staged %d wheelhouse files", len(wheelhouseEntry.Files))
			}
		} else {
			log.Printf("all discovered pip requirements are satisfied by bundled distro packages; skipping wheelhouse download")
		}
	}

	// Stage templates.
	var templatesEntry *bundle.TemplatesEntry
	if spec.TemplatesDir != "" {
		templatesEntry, err = builder.StageTemplates(spec.TemplatesDir, stageDir)
		if err != nil {
			return err
		}
		if templatesEntry != nil {
			log.Printf("staged %d template files", len(templatesEntry.Files))
		}
	}

	// Clone helm chart repositories.
	var helmChartsEntries []bundle.HelmChartsEntry
	if len(spec.HelmCharts) > 0 {
		log.Printf("cloning %d helm chart repositories...", len(spec.HelmCharts))
		helmBinary := ""
		if helmEntry != nil {
			helmBinary = filepath.Join(stageDir, "helm", "helm")
		}
		helmChartsEntries, err = builder.BuildHelmCharts(ctx, spec.HelmCharts, stageDir, helmBinary)
		if err != nil {
			return err
		}
		for _, hc := range helmChartsEntries {
			log.Printf("helm chart %q staged at %s (sha %s)", hc.Name, hc.Path, hc.ResolvedSHA[:min(12, len(hc.ResolvedSHA))])
		}
	}

	// Resolve the set of container images to include in the bundle.
	var imagesEntry *bundle.ImagesEntry
	if spec.Images != nil {
		refs, err := resolveImageRefs(spec.Images, helmChartsEntries, stageDir)
		if err != nil {
			return err
		}
		if len(refs) > 0 {
			log.Printf("pulling %d container images...", len(refs))
			imagesEntry, err = builder.BuildImages(ctx, refs, stageDir)
			if err != nil {
				return err
			}
			log.Printf("staged %d images", len(imagesEntry.Images))
		}
	}

	// Generate and write manifest.
	manifest := builder.BuildManifest(spec, gitSHA, builder.ManifestInputs{
		RKE2:       rke2Entry,
		Helm:       helmEntry,
		Wheelhouse: wheelhouseEntry,
		AetherOps:  aetherOpsEntry,
		Debs:       debEntries,
		Templates:  templatesEntry,
		Onramp:     onrampEntry,
		HelmCharts: helmChartsEntries,
		Images:     imagesEntry,
	})
	manifestPath := filepath.Join(stageDir, "manifest.json")
	if err := bundle.Write(manifestPath, manifest); err != nil {
		return err
	}

	// Create archive.
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return err
	}
	if err := builder.Archive(stageDir, outputPath); err != nil {
		return err
	}

	// Write bundle checksum sidecar.
	hash, err := builder.WriteBundleChecksum(outputPath)
	if err != nil {
		return err
	}

	logDynamicDiscoveryAdvisory(discoveredDebs, discoveredAptSources)
	logBuildSummary(outputPath, hash, rke2Entry, helmEntry, aetherOpsEntry, onrampEntry, debEntries, wheelhouseEntry, templatesEntry, helmChartsEntries, imagesEntry)
	return nil
}

func mergeDebSpecs(explicit []bundle.DebSpec, scan *builder.OnrampDependencyScan) ([]bundle.DebSpec, []string) {
	seen := make(map[string]bundle.DebSpec, len(explicit))
	for _, deb := range explicit {
		seen[deb.Name] = deb
	}
	var discovered []string
	if scan != nil {
		for _, name := range scan.AptPackages {
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = bundle.DebSpec{Name: name}
			discovered = append(discovered, name)
		}
	}

	out := make([]bundle.DebSpec, 0, len(seen))
	for _, deb := range seen {
		out = append(out, deb)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	sort.Strings(discovered)
	return out, discovered
}

func mergeAptSources(explicit []bundle.AptSourceSpec, scan *builder.OnrampDependencyScan) ([]bundle.AptSourceSpec, []bundle.AptSourceSpec) {
	seen := make(map[string]bundle.AptSourceSpec, len(explicit))
	for _, src := range explicit {
		seen[src.Name] = src
	}
	var discovered []bundle.AptSourceSpec
	if scan != nil {
		for _, src := range scan.AptRepositories {
			if _, ok := seen[src.Name]; ok {
				continue
			}
			seen[src.Name] = src
			discovered = append(discovered, src)
		}
	}
	out := make([]bundle.AptSourceSpec, 0, len(seen))
	for _, src := range seen {
		out = append(out, src)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	sort.Slice(discovered, func(i, j int) bool { return discovered[i].Name < discovered[j].Name })
	return out, discovered
}

func formatMissingDebsError(suites []string, effective []bundle.DebSpec, discovered []string, discoveredSources []bundle.AptSourceSpec, scan *builder.OnrampDependencyScan, missing []string) error {
	explicitSet := make(map[string]bool, len(effective))
	for _, d := range effective {
		explicitSet[d.Name] = true
	}
	discoveredSet := make(map[string]bool, len(discovered))
	for _, name := range discovered {
		discoveredSet[name] = true
	}

	var missingExplicit []string
	var missingDiscovered []string
	for _, name := range missing {
		if discoveredSet[name] {
			missingDiscovered = append(missingDiscovered, name)
			continue
		}
		if explicitSet[name] {
			missingExplicit = append(missingExplicit, name)
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "some requested .deb packages could not be resolved from the Ubuntu package indexes for suites %s.\n", strings.Join(suites, ", "))
	if len(discoveredSources) > 0 {
		fmt.Fprintf(&b, "\nAuto-discovered apt repositories added for this build (%d):\n", len(discoveredSources))
		for _, src := range discoveredSources {
			fmt.Fprintf(&b, "- %s (%s)\n", src.Name, src.URL)
			if scan != nil && len(scan.AptRepoSources[src.Name]) > 0 {
				fmt.Fprintf(&b, "  discovered from:\n")
				for _, ref := range scan.AptRepoSources[src.Name] {
					fmt.Fprintf(&b, "  - %s\n", ref)
				}
			}
		}
	}
	if len(discovered) > 0 {
		fmt.Fprintf(&b, "\nAuto-discovered apt packages added for this build (%d):\n- %s\n", len(discovered), strings.Join(discovered, "\n- "))
	}
	if len(missingDiscovered) > 0 {
		fmt.Fprintf(&b, "\nAuto-discovered packages not found in the Ubuntu indexes:\n")
		for _, name := range missingDiscovered {
			fmt.Fprintf(&b, "- %s", name)
			if scan != nil && len(scan.AptSources[name]) > 0 {
				fmt.Fprintf(&b, "\n  discovered from:\n")
				for _, src := range scan.AptSources[name] {
					fmt.Fprintf(&b, "  - %s\n", src)
				}
			} else {
				fmt.Fprintf(&b, "\n")
			}
		}
	}
	if len(missingExplicit) > 0 {
		fmt.Fprintf(&b, "\nExplicit spec packages not found in the Ubuntu indexes:\n- %s\n", strings.Join(missingExplicit, "\n- "))
	}
	fmt.Fprintf(&b, "\nThis usually means the package comes from a third-party repository (for example Docker packages like containerd.io) rather than the Ubuntu archive. When that happens, dynamically adding the package name is not enough; bootstrap needs explicit handling for that package source or an Ubuntu-native alternative package name.\n\nConsider adding the discovered repositories and first-level package dependencies to bundle.yaml so the bundle spec explicitly reflects the upstream dependency sources, even though bootstrap will continue resolving deeper dependency chains automatically.")
	return errors.New(b.String())
}

func logDynamicDiscoveryAdvisory(discoveredDebs []string, discoveredSources []bundle.AptSourceSpec) {
	if len(discoveredDebs) == 0 && len(discoveredSources) == 0 {
		return
	}

	log.Printf("")
	log.Printf("=== Dependency Discovery Summary ===")
	log.Printf("The bundle build succeeded, but first-level upstream dependencies were discovered dynamically.")
	log.Printf("Consider adding them to bundle.yaml so the spec explicitly captures the intended dependency sources.")
	if len(discoveredSources) > 0 {
		log.Printf("")
		log.Printf("Apt Repositories Discovered:")
		for _, src := range discoveredSources {
			log.Printf("  %s:", displaySourceName(src.Name))
			log.Printf("    name: %s", src.Name)
			log.Printf("    url: %s", src.URL)
			log.Printf("    components: [%s]", strings.Join(src.Components, ", "))
			if len(src.Suites) > 0 {
				log.Printf("    suites: [%s]", strings.Join(src.Suites, ", "))
			} else {
				log.Printf("    suites: <inherits ubuntu.suites>")
			}
			if len(src.Architectures) > 0 {
				log.Printf("    architectures: [%s]", strings.Join(src.Architectures, ", "))
			} else {
				log.Printf("    architectures: <inherits ubuntu.architectures>")
			}
			if src.KeyURL != "" {
				log.Printf("    key_url: %s", src.KeyURL)
			}
		}
	}
	if len(discoveredDebs) > 0 {
		log.Printf("")
		log.Printf("Apt Packages Discovered:")
		for _, name := range discoveredDebs {
			log.Printf("  - %s", name)
		}
	}
	log.Printf("")
	log.Printf("=== End Dependency Discovery Summary ===")
}

func displaySourceName(name string) string {
	if name == "" {
		return "Repository"
	}
	parts := strings.FieldsFunc(name, func(r rune) bool {
		return r == '-' || r == '_' || r == '.'
	})
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	if len(parts) == 0 {
		return name
	}
	return strings.Join(parts, " ")
}

func logBuildSummary(
	outputPath, hash string,
	rke2Entry *bundle.RKE2Entry,
	helmEntry *bundle.HelmEntry,
	aetherOpsEntry *bundle.AetherOpsEntry,
	onrampEntry *bundle.OnrampEntry,
	debEntries []bundle.DebEntry,
	wheelhouseEntry *bundle.WheelhouseEntry,
	templatesEntry *bundle.TemplatesEntry,
	helmChartsEntries []bundle.HelmChartsEntry,
	imagesEntry *bundle.ImagesEntry,
) {
	log.Printf("")
	log.Printf("=== Bundle Build Summary ===")
	log.Printf("Bundle Path: %s", outputPath)
	log.Printf("Bundle SHA256: %s", hash)
	log.Printf("")
	log.Printf("Components:")
	log.Printf("  .deb packages: %d", len(debEntries))

	if rke2Entry != nil {
		log.Printf("  RKE2 artifacts: %d", len(rke2Entry.Artifacts))
	}
	if helmEntry != nil {
		log.Printf("  Helm files: %d", len(helmEntry.Files))
	}
	if aetherOpsEntry != nil {
		log.Printf("  aether-ops files: %d", len(aetherOpsEntry.Files))
	}
	if onrampEntry != nil {
		log.Printf("  aether-onramp files: %d", len(onrampEntry.Files))
	}
	if wheelhouseEntry != nil {
		log.Printf("  wheelhouse requirements: %d", len(wheelhouseEntry.Requirements))
		log.Printf("  wheelhouse files: %d", len(wheelhouseEntry.Files))
	}
	if templatesEntry != nil {
		log.Printf("  template files: %d", len(templatesEntry.Files))
	}
	if len(helmChartsEntries) > 0 {
		log.Printf("  Helm chart repositories: %d", len(helmChartsEntries))
	}
	if imagesEntry != nil {
		log.Printf("  container images: %d", len(imagesEntry.Images))
	}

	log.Printf("")
	log.Printf("=== End Bundle Build Summary ===")
}

// resolveImageRefs computes the final set of image references to pull
// for a given spec. When auto_extract is true, it walks each cloned helm
// chart's values.yaml files and unions the discovered set with the
// operator-provided Extra list. When auto_extract is false, it simply
// returns the explicit List from the spec. Entries in Exclude are then
// removed — useful for skipping images that cannot be pulled (for
// example, legacy Docker v1 manifests).
func resolveImageRefs(spec *bundle.ImagesSpec, charts []bundle.HelmChartsEntry, stageDir string) ([]string, error) {
	var refs []string
	if spec.AutoExtract {
		for _, hc := range charts {
			chartDir := filepath.Join(stageDir, hc.Path)
			extracted, err := builder.ExtractImagesFromChart(chartDir)
			if err != nil {
				return nil, err
			}
			log.Printf("extracted %d image refs from chart %q", len(extracted), hc.Name)
			refs = append(refs, extracted...)
		}
		refs = append(refs, spec.Extra...)
	} else {
		refs = spec.List
	}

	if len(spec.Exclude) == 0 {
		return refs, nil
	}
	excluded := map[string]bool{}
	for _, e := range spec.Exclude {
		excluded[e] = true
	}
	out := refs[:0]
	for _, r := range refs {
		if excluded[r] {
			log.Printf("excluding image %s (listed in images.exclude)", r)
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
