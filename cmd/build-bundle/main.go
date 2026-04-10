package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aether-gui/aether-ops-bootstrap/internal/builder"
	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
)

var gitSHA string // set via ldflags: -X main.gitSHA=...

func main() {
	specPath := flag.String("spec", "bundle.yaml", "path to spec file or directory of spec files")
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

	// Fetch .deb packages.
	var debEntries []bundle.DebEntry
	if len(spec.Debs) > 0 {
		log.Printf("resolving and fetching .deb packages...")
		debEntries, err = builder.FetchDebs(ctx, dl, spec, stageDir)
		if err != nil {
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

	// Clone aether-onramp into the bundle for airgap deployments.
	var onrampEntry *bundle.OnrampEntry
	if spec.Onramp != nil {
		log.Printf("cloning aether-onramp from %s...", spec.Onramp.Repo)
		onrampEntry, err = builder.BuildOnramp(ctx, spec.Onramp, stageDir)
		if err != nil {
			return err
		}
		log.Printf("onramp staged at %s (sha %s, %d files)", onrampEntry.Path, onrampEntry.ResolvedSHA[:min(12, len(onrampEntry.ResolvedSHA))], len(onrampEntry.Files))
	}

	// Clone helm chart repositories.
	var helmChartsEntries []bundle.HelmChartsEntry
	if len(spec.HelmCharts) > 0 {
		log.Printf("cloning %d helm chart repositories...", len(spec.HelmCharts))
		helmChartsEntries, err = builder.BuildHelmCharts(ctx, spec.HelmCharts, stageDir)
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

	log.Printf("bundle written to %s (SHA256: %s)", outputPath, hash)
	return nil
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
