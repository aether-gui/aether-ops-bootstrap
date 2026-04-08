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
	entries, err := os.ReadDir(*specPath)
	if err != nil {
		log.Fatalf("reading spec directory: %v", err)
	}

	outputDir := *output
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

	// Generate and write manifest.
	manifest := builder.BuildManifest(spec, gitSHA, rke2Entry, aetherOpsEntry, debEntries, templatesEntry)
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
