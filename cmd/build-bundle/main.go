package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/aether-gui/aether-ops-bootstrap/internal/builder"
	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
)

func main() {
	specPath := flag.String("spec", "bundle.yaml", "path to the bundle spec file")
	output := flag.String("output", "dist/bundle.tar.zst", "output path for the bundle archive")
	flag.Parse()

	// Parse and validate spec.
	spec, err := bundle.ParseSpec(*specPath)
	if err != nil {
		log.Fatalf("parsing spec: %v", err)
	}
	if err := bundle.ValidateSpec(spec); err != nil {
		log.Fatalf("validating spec: %v", err)
	}

	// Create temp staging directory.
	stageDir, err := os.MkdirTemp("", "aether-bundle-*")
	if err != nil {
		log.Fatalf("creating staging directory: %v", err)
	}
	defer os.RemoveAll(stageDir)

	ctx := context.Background()
	dl := &builder.Downloader{Client: http.DefaultClient}

	// Build RKE2 artifacts.
	var rke2Entry *bundle.RKE2Entry
	if spec.RKE2 != nil {
		log.Printf("fetching RKE2 %s artifacts...", spec.RKE2.Version)
		rke2Entry, err = builder.FetchAndVerifyRKE2(ctx, dl, spec.RKE2, spec.Ubuntu.Architectures, stageDir)
		if err != nil {
			log.Fatalf("building RKE2: %v", err)
		}
		log.Printf("RKE2 artifacts staged (%d files)", len(rke2Entry.Artifacts))
	}

	// Generate and write manifest.
	manifest := builder.BuildManifest(spec, rke2Entry)
	manifestPath := filepath.Join(stageDir, "manifest.json")
	if err := bundle.Write(manifestPath, manifest); err != nil {
		log.Fatalf("writing manifest: %v", err)
	}

	// Create archive.
	if err := os.MkdirAll(filepath.Dir(*output), 0755); err != nil {
		log.Fatalf("creating output directory: %v", err)
	}
	if err := builder.Archive(stageDir, *output); err != nil {
		log.Fatalf("creating archive: %v", err)
	}

	log.Printf("bundle written to %s", *output)
}
