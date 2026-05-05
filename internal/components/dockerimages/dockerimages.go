// Package dockerimages loads the bundle's container image tarballs into
// Docker's image store at install time.
//
// The rke2 component stages those same tarballs into
// /var/lib/rancher/rke2/agent/images/ for containerd's airgap loader,
// but containerd and Docker keep separate image stores. OnRamp roles
// that drive Docker directly (ocudu, gnbsim, oai, srsran, n3iwf,
// oscric, plus the gNB Docker preflight) can't see anything that only
// landed in containerd. This component closes the gap.
//
// On Kubernetes-only hosts (no Docker daemon active) the component
// logs a warning and skips. Per-tarball load failures are warned but
// do not fail the install — a single bad tarball shouldn't block
// every other image load behind it.
package dockerimages

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"path/filepath"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
	"github.com/aether-gui/aether-ops-bootstrap/internal/cmdutil"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components"
	"github.com/aether-gui/aether-ops-bootstrap/internal/state"
)

// Component loads bundled container image tarballs into Docker.
type Component struct {
	extractDir string
	manifest   *bundle.Manifest
}

// New constructs a dockerimages component reading from extractDir.
func New(extractDir string, manifest *bundle.Manifest) *Component {
	return &Component{extractDir: extractDir, manifest: manifest}
}

func (c *Component) Name() string { return "dockerimages" }

// DesiredVersion is the bundle version when there are images to load,
// empty otherwise (which makes Plan a NoOp).
func (c *Component) DesiredVersion(b *bundle.Manifest) string {
	if b == nil || b.Components.Images == nil || len(b.Components.Images.Images) == 0 {
		return ""
	}
	return b.BundleVersion
}

func (c *Component) CurrentVersion(s *state.State) string {
	if cs, ok := s.Components["dockerimages"]; ok {
		return cs.Version
	}
	return ""
}

func (c *Component) Plan(current, desired string) (components.Plan, error) {
	if desired == "" || current == desired || c.manifest == nil || c.manifest.Components.Images == nil {
		return components.Plan{NoOp: true}, nil
	}
	images := c.manifest.Components.Images.Images
	if len(images) == 0 {
		return components.Plan{NoOp: true}, nil
	}
	actions := []components.Action{
		{
			Description: fmt.Sprintf("load %d images into Docker", len(images)),
			Fn: func(ctx context.Context) error {
				return c.loadImages(ctx, images)
			},
		},
	}
	return components.Plan{Actions: actions}, nil
}

func (c *Component) Apply(ctx context.Context, plan components.Plan) error {
	return components.ApplyPlan(ctx, c.Name(), plan)
}

func (c *Component) loadImages(ctx context.Context, images []bundle.ImageArtifact) error {
	if !dockerAvailable(ctx) {
		log.Printf("  Docker daemon not available; skipping docker load (Kubernetes-only host)")
		return nil
	}
	loaded := 0
	for _, img := range images {
		path := filepath.Join(c.extractDir, img.Path)
		cmd := exec.CommandContext(ctx, "docker", "load", "-i", path)
		if out, err := cmdutil.Run(ctx, cmd); err != nil {
			log.Printf("  warning: docker load %s failed: %v\n%s", filepath.Base(path), err, out)
			continue
		}
		log.Printf("  loaded %s into Docker", img.Ref)
		loaded++
	}
	log.Printf("  loaded %d/%d bundled images into Docker", loaded, len(images))
	return nil
}

// dockerAvailable returns true when `docker info` succeeds — i.e. the
// daemon is reachable. A failure means we silently skip the load step,
// which is the right behaviour on Kubernetes-only hosts that have
// docker-ce installed but the daemon disabled.
func dockerAvailable(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, "docker", "info")
	return cmd.Run() == nil
}
