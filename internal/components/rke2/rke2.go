package rke2

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components"
	"github.com/aether-gui/aether-ops-bootstrap/internal/state"
	"github.com/aether-gui/aether-ops-bootstrap/internal/systemd"
)

const (
	rke2ConfigDir = "/etc/rancher/rke2"
	rke2ImageDir  = "/var/lib/rancher/rke2/agent/images"
	profileDir    = "/etc/profile.d"
)

// Component installs RKE2 from vendored tarballs and manages its systemd service.
type Component struct {
	extractDir string
	manifest   *bundle.Manifest
}

func New(extractDir string, manifest *bundle.Manifest) *Component {
	return &Component{extractDir: extractDir, manifest: manifest}
}

func (c *Component) Name() string { return "rke2" }

func (c *Component) DesiredVersion(b *bundle.Manifest) string {
	if b.Components.RKE2 == nil {
		return ""
	}
	return b.Components.RKE2.Version
}

func (c *Component) CurrentVersion(s *state.State) string {
	if cs, ok := s.Components["rke2"]; ok {
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

	if c.manifest == nil {
		return components.Plan{NoOp: true}, nil
	}
	rke2 := c.manifest.Components.RKE2
	if rke2 == nil {
		return components.Plan{NoOp: true}, nil
	}

	// Check if RKE2 is already running at the desired version.
	// This handles re-runs where a prior install started RKE2 but
	// didn't complete (e.g., timeout on readiness check).
	mgr := &systemd.SystemctlManager{}
	status, _ := mgr.Status(context.Background(), "rke2-server")
	if status.ActiveState == "active" {
		cmd := exec.CommandContext(context.Background(), "/usr/local/bin/rke2", "--version")
		if out, err := cmd.Output(); err == nil {
			if strings.Contains(string(out), desired) {
				log.Printf("  rke2-server already running at %s", desired)
				// Ensure kubectl symlink exists even if prior install was interrupted.
				src := "/var/lib/rancher/rke2/bin/kubectl"
				dst := "/usr/local/bin/kubectl"
				if _, err := os.Stat(dst); os.IsNotExist(err) {
					_ = os.Symlink(src, dst)
					log.Printf("  symlinked kubectl")
				}
				return components.Plan{NoOp: true}, nil
			}
		}
	}

	actions := []components.Action{
		{
			Description: "write RKE2 config",
			Fn:          func(ctx context.Context) error { return c.writeConfig() },
		},
		{
			Description: fmt.Sprintf("extract RKE2 %s", desired),
			Fn:          func(ctx context.Context) error { return c.extractBinary(rke2) },
		},
		{
			Description: "stage airgap images",
			Fn:          func(ctx context.Context) error { return c.stageImages(rke2) },
		},
		{
			Description: "install profile drop-in",
			Fn:          func(ctx context.Context) error { return c.installProfile() },
		},
		{
			Description: "enable and start rke2-server",
			Fn: func(ctx context.Context) error {
				mgr := &systemd.SystemctlManager{}
				if err := mgr.DaemonReload(ctx); err != nil {
					return err
				}
				if err := mgr.Enable(ctx, "rke2-server"); err != nil {
					return err
				}
				if err := mgr.Start(ctx, "rke2-server"); err != nil {
					return err
				}
				log.Printf("  rke2-server started")
				return nil
			},
		},
		{
			Description: "wait for RKE2 ready",
			Fn:          func(ctx context.Context) error { return c.waitForReady(ctx) },
		},
		{
			Description: "symlink kubectl",
			Fn: func(ctx context.Context) error {
				src := "/var/lib/rancher/rke2/bin/kubectl"
				dst := "/usr/local/bin/kubectl"
				os.Remove(dst) // remove stale symlink if exists
				if err := os.Symlink(src, dst); err != nil {
					return fmt.Errorf("symlink kubectl: %w", err)
				}
				log.Printf("  symlinked %s → %s", src, dst)
				return nil
			},
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

func (c *Component) writeConfig() error {
	if err := os.MkdirAll(rke2ConfigDir, 0755); err != nil {
		return err
	}

	tmplPath := filepath.Join(c.extractDir, "templates", "rke2-config.yaml.tmpl")
	raw, err := os.ReadFile(tmplPath)
	if err != nil {
		return fmt.Errorf("reading RKE2 config template: %w", err)
	}

	tmpl, err := template.New("rke2-config").Parse(string(raw))
	if err != nil {
		return fmt.Errorf("parsing RKE2 config template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, nil); err != nil {
		return err
	}

	configPath := filepath.Join(rke2ConfigDir, "config.yaml")
	if err := os.WriteFile(configPath, buf.Bytes(), 0644); err != nil {
		return err
	}
	log.Printf("  wrote %s", configPath)
	return nil
}

func (c *Component) extractBinary(rke2 *bundle.RKE2Entry) error {
	// Find the binary artifact.
	var binaryPath string
	for _, a := range rke2.Artifacts {
		if a.Type == "binary" {
			binaryPath = filepath.Join(c.extractDir, a.Path)
			break
		}
	}
	if binaryPath == "" {
		return fmt.Errorf("no RKE2 binary artifact in manifest")
	}

	// Extract the tar.gz to /usr/local.
	log.Printf("  extracting %s to /usr/local", filepath.Base(binaryPath))
	f, err := os.Open(binaryPath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		target := filepath.Join("/usr/local", header.Name)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(out, tr)
			closeErr := out.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		}
	}

	return nil
}

func (c *Component) stageImages(rke2 *bundle.RKE2Entry) error {
	if err := os.MkdirAll(rke2ImageDir, 0755); err != nil {
		return err
	}

	for _, a := range rke2.Artifacts {
		if a.Type != "images" {
			continue
		}
		srcPath := filepath.Join(c.extractDir, a.Path)
		dstPath := filepath.Join(rke2ImageDir, filepath.Base(a.Path))

		log.Printf("  staging %s", filepath.Base(a.Path))
		src, err := os.Open(srcPath)
		if err != nil {
			return err
		}
		dst, err := os.Create(dstPath)
		if err != nil {
			src.Close()
			return err
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
	}

	return nil
}

func (c *Component) installProfile() error {
	srcPath := filepath.Join(c.extractDir, "templates", "profile.d", "rke2.sh")
	if _, err := os.Stat(srcPath); os.IsNotExist(err) {
		return nil
	}

	if err := os.MkdirAll(profileDir, 0755); err != nil {
		return err
	}

	data, err := os.ReadFile(srcPath)
	if err != nil {
		return err
	}

	destPath := filepath.Join(profileDir, "rke2.sh")
	if err := os.WriteFile(destPath, data, 0644); err != nil {
		return err
	}
	log.Printf("  wrote %s", destPath)
	return nil
}

func (c *Component) waitForReady(ctx context.Context) error {
	kubeconfigPath := filepath.Join(rke2ConfigDir, "rke2.yaml")

	// Wait for kubeconfig to appear.
	log.Printf("  waiting for kubeconfig at %s", kubeconfigPath)
	deadline := time.Now().Add(5 * time.Minute)
	for {
		if _, err := os.Stat(kubeconfigPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for kubeconfig at %s", kubeconfigPath)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		time.Sleep(2 * time.Second)
	}

	// Wait for kubectl to work (proves API server + auth are ready).
	log.Printf("  waiting for kubectl get nodes to succeed")
	kubectlPath := "/var/lib/rancher/rke2/bin/kubectl"
	deadline = time.Now().Add(10 * time.Minute)
	for {
		cmd := exec.CommandContext(ctx, kubectlPath,
			"--kubeconfig", kubeconfigPath,
			"get", "nodes", "--no-headers")
		output, err := cmd.Output()
		if err == nil && len(output) > 0 {
			log.Printf("  Kubernetes API ready (node visible)")
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for Kubernetes API readiness")
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		time.Sleep(5 * time.Second)
	}
}
