package aetherops

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components"
	"github.com/aether-gui/aether-ops-bootstrap/internal/state"
	"github.com/aether-gui/aether-ops-bootstrap/internal/systemd"
)

const (
	binaryPath  = "/usr/local/bin/aether-ops"
	servicePath = "/etc/systemd/system/aether-ops.service"
	configDir   = "/etc/aether-ops"
	stateDir    = "/var/lib/aether-ops"
	healthURL   = "http://127.0.0.1:8186/healthz"
)

// Component installs the aether-ops daemon binary, systemd unit, and config.
type Component struct {
	extractDir string
	manifest   *bundle.Manifest
}

func New(extractDir string, manifest *bundle.Manifest) *Component {
	return &Component{extractDir: extractDir, manifest: manifest}
}

func (c *Component) Name() string { return "aether_ops" }

func (c *Component) DesiredVersion(b *bundle.Manifest) string {
	if b.Components.AetherOps == nil {
		return ""
	}
	return b.Components.AetherOps.Version
}

func (c *Component) CurrentVersion(s *state.State) string {
	if cs, ok := s.Components["aether_ops"]; ok {
		return cs.Version
	}
	return ""
}

func (c *Component) Plan(current, desired string) (components.Plan, error) {
	if desired == "" {
		return components.Plan{NoOp: true}, nil
	}
	if current == desired {
		return components.Plan{NoOp: true}, nil
	}
	if c.manifest == nil || c.manifest.Components.AetherOps == nil {
		return components.Plan{NoOp: true}, nil
	}

	aops := c.manifest.Components.AetherOps

	actions := []components.Action{
		{
			Description: "stop aether-ops if running",
			Fn: func(ctx context.Context) error {
				mgr := &systemd.SystemctlManager{}
				status, _ := mgr.Status(ctx, "aether-ops")
				if status.ActiveState == "active" {
					log.Printf("  stopping aether-ops for upgrade")
					return mgr.Stop(ctx, "aether-ops")
				}
				return nil
			},
		},
		{
			Description: "create config and state directories",
			Fn: func(ctx context.Context) error {
				for _, dir := range []string{configDir, stateDir} {
					if err := os.MkdirAll(dir, 0750); err != nil {
						return err
					}
				}
				log.Printf("  created %s and %s", configDir, stateDir)
				return nil
			},
		},
		{
			Description: fmt.Sprintf("install aether-ops %s", desired),
			Fn: func(ctx context.Context) error {
				return c.installFiles(aops)
			},
		},
		{
			Description: "enable and start aether-ops",
			Fn: func(ctx context.Context) error {
				mgr := &systemd.SystemctlManager{}
				if err := mgr.DaemonReload(ctx); err != nil {
					return err
				}
				if err := mgr.Enable(ctx, "aether-ops"); err != nil {
					return err
				}
				if err := mgr.Start(ctx, "aether-ops"); err != nil {
					return err
				}
				log.Printf("  aether-ops started")
				return nil
			},
		},
		{
			Description: "wait for aether-ops health",
			Fn:          func(ctx context.Context) error { return c.waitForHealth(ctx) },
		},
	}

	return components.Plan{Actions: actions}, nil
}

func (c *Component) Apply(ctx context.Context, plan components.Plan) error {
	for _, action := range plan.Actions {
		if err := action.Fn(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (c *Component) installFiles(aops *bundle.AetherOpsEntry) error {
	for _, f := range aops.Files {
		srcPath := filepath.Join(c.extractDir, f.Path)
		var dstPath string
		var mode os.FileMode

		switch filepath.Base(f.Path) {
		case "aether-ops":
			dstPath = binaryPath
			mode = 0755
		case "aether-ops.service":
			dstPath = servicePath
			mode = 0644
		default:
			log.Printf("  skipping unknown file %s", f.Path)
			continue
		}

		src, err := os.Open(srcPath)
		if err != nil {
			return fmt.Errorf("opening %s: %w", f.Path, err)
		}

		dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
		if err != nil {
			src.Close()
			return fmt.Errorf("creating %s: %w", dstPath, err)
		}

		_, copyErr := io.Copy(dst, src)
		src.Close()
		closeErr := dst.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}

		log.Printf("  installed %s", dstPath)
	}

	return nil
}

func (c *Component) waitForHealth(ctx context.Context) error {
	log.Printf("  waiting for aether-ops at %s", healthURL)
	client := &http.Client{Timeout: 5 * time.Second}

	deadline := time.Now().Add(2 * time.Minute)
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				log.Printf("  aether-ops healthy")
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for aether-ops health at %s", healthURL)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		time.Sleep(2 * time.Second)
	}
}
