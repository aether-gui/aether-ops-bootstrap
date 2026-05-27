package aetherops

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"time"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components"
	"github.com/aether-gui/aether-ops-bootstrap/internal/installctx"
	"github.com/aether-gui/aether-ops-bootstrap/internal/state"
	"github.com/aether-gui/aether-ops-bootstrap/internal/systemd"
)

const (
	binaryPath         = "/usr/local/bin/aether-ops"
	servicePath        = "/etc/systemd/system/aether-ops.service"
	configDir          = "/etc/aether-ops"
	stateDir           = "/var/lib/aether-ops"
	onrampPasswordPath = configDir + "/onramp-password"
	daemonGroup        = "aether-ops"
	healthURL          = "http://127.0.0.1:8186/healthz"
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
				// The daemon runs as aether-ops and must traverse
				// configDir to read onramp-password (see stampCredentials
				// in the aether-ops daemon). Group-owned by aether-ops
				// with mode 0750 lets the daemon read the dropins
				// without exposing them world-wide.
				if err := chgrpDaemon(configDir); err != nil {
					return fmt.Errorf("chgrp %s: %w", configDir, err)
				}
				log.Printf("  created %s and %s", configDir, stateDir)
				return nil
			},
		},
		{
			Description: "write onramp password file",
			Fn: func(ctx context.Context) error {
				password := installctx.OnrampPasswordFromContext(ctx)
				if password == "" {
					return fmt.Errorf("onramp password missing on context; launcher should resolve it before Apply")
				}
				return writeOnrampPasswordFile(onrampPasswordPath, password)
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
	return components.ApplyPlan(ctx, c.Name(), plan)
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

// chgrpDaemon changes the group of path to the aether-ops daemon group
// without touching the owner. The daemon needs to traverse configDir to
// read onramp-password (and any future dropins); a chgrp keeps root as
// the owner — only the bootstrap and the operator should be able to
// write — while granting the daemon group read+exec on the directory.
func chgrpDaemon(path string) error {
	gid, err := lookupDaemonGID()
	if err != nil {
		return err
	}
	return os.Lchown(path, -1, gid)
}

// writeOnrampPasswordFile persists the resolved onramp password to path
// so the aether-ops daemon's stampCredentials can re-overlay
// ansible_user / ansible_password into hosts.ini on every inventory
// sync (ADR-0010 round-trip). Without this file the daemon's first
// sync would silently drop the credentials the onramp component
// stamped, leaving ansible unable to authenticate against the onramp
// user.
//
// Mode 0640 root:aether-ops: only the daemon and root can read; nothing
// else on the host should see this secret. No trailing newline — matches
// what aether-onramp's install.sh writes and avoids a chpasswd-style
// truncation surprise in the daemon read path.
func writeOnrampPasswordFile(path, password string) error {
	gid, err := lookupDaemonGID()
	if err != nil {
		return err
	}
	return writeOnrampPasswordFileAs(path, password, 0, gid)
}

// writeOnrampPasswordFileAs is the test-friendly worker behind
// writeOnrampPasswordFile. Takes explicit uid/gid so unit tests can
// exercise the write+chmod path without a real aether-ops group on
// the host. Tests that cannot chown (non-root) should pass -1 for
// uid and gid to skip ownership changes.
func writeOnrampPasswordFileAs(path, password string, uid, gid int) error {
	if err := installctx.ValidateOnrampPassword(password); err != nil {
		return fmt.Errorf("onramp password: %w", err)
	}
	if err := os.WriteFile(path, []byte(password), 0640); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	// WriteFile only applies mode on file creation; tighten unconditionally
	// in case the file pre-existed with a looser mode.
	if err := os.Chmod(path, 0640); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	if uid >= 0 || gid >= 0 {
		if err := os.Lchown(path, uid, gid); err != nil {
			return fmt.Errorf("chown %s: %w", path, err)
		}
	}
	log.Printf("  wrote %s (mode 0640 root:%s)", path, daemonGroup)
	return nil
}

// lookupDaemonGID resolves the GID of the aether-ops service group.
// Returned as int so callers can pass it straight to os.Lchown.
func lookupDaemonGID() (int, error) {
	g, err := user.LookupGroup(daemonGroup)
	if err != nil {
		return 0, fmt.Errorf("lookup %s group: %w", daemonGroup, err)
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return 0, fmt.Errorf("parse %s gid: %w", daemonGroup, err)
	}
	return gid, nil
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
