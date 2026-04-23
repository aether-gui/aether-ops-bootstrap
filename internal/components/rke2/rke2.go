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
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"text/template"
	"time"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components"
	"github.com/aether-gui/aether-ops-bootstrap/internal/state"
	"github.com/aether-gui/aether-ops-bootstrap/internal/systemd"
)

// userLookup resolves a Unix username to a user.User record. Extracted so the
// rke2 component's kubeconfig installer can use it without depending on the
// serviceaccount package.
func userLookup(name string) (*user.User, error) {
	return user.Lookup(name)
}

// chownPath sets ownership of path to u's uid/gid. The uid/gid fields on
// user.User are numeric strings; we parse them once per call.
func chownPath(path string, u *user.User) error {
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return fmt.Errorf("parse uid %q for %s: %w", u.Uid, u.Username, err)
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return fmt.Errorf("parse gid %q for %s: %w", u.Gid, u.Username, err)
	}
	if err := syscall.Chown(path, uid, gid); err != nil {
		return fmt.Errorf("chown %s to %s: %w", path, u.Username, err)
	}
	return nil
}

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
			Description: "stage bundled container images",
			Fn:          func(ctx context.Context) error { return c.stageAppImages() },
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
				// Only remove if it's a symlink — don't delete a real binary.
				if info, err := os.Lstat(dst); err == nil {
					if info.Mode()&os.ModeSymlink != 0 {
						os.Remove(dst)
					} else {
						return fmt.Errorf("symlink kubectl: %s exists and is not a symlink", dst)
					}
				}
				if err := os.Symlink(src, dst); err != nil {
					return fmt.Errorf("symlink kubectl: %w", err)
				}
				log.Printf("  symlinked %s → %s", src, dst)
				return nil
			},
		},
		// Upstream onramp's 5gc core role uses kubernetes.core.helm,
		// which reads ~/.kube/config for the ansible user. When onramp
		// also provisions k8s its rke2 role copies rke2.yaml into that
		// path — but bootstrap short-circuits the k8s role by staging
		// RKE2 itself, so the copy never happens and helm falls back
		// to http://localhost:8080. Replicate the copy here.
		{
			Description: "install kubeconfig for onramp user",
			Fn: func(ctx context.Context) error {
				return c.installOnrampKubeconfig()
			},
		},
	}

	return components.Plan{Actions: actions}, nil
}

func (c *Component) Apply(ctx context.Context, plan components.Plan) error {
	return components.ApplyPlan(ctx, c.Name(), plan)
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

		// Validate path — reject absolute paths, traversal, and symlinks.
		clean := filepath.Clean(header.Name)
		if filepath.IsAbs(clean) || strings.Contains(clean, "..") {
			return fmt.Errorf("invalid tar entry path: %s", header.Name)
		}
		if header.Typeflag == tar.TypeSymlink || header.Typeflag == tar.TypeLink {
			continue // skip symlinks/hardlinks for security
		}
		target := filepath.Join("/usr/local", clean)

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

// stageAppImages copies container image tarballs bundled alongside RKE2
// (under images/ in the bundle root) into rke2's airgap image directory.
// RKE2 loads every tarball it finds there on startup, which means any
// pre-pulled application images become available to the cluster without
// external registry access. This is how we deliver SD-Core and other
// workload images in a fully airgapped bundle.
//
// Re-runs are idempotent: existing files with the same name are
// truncated and rewritten. Missing bundle data is a silent no-op so
// bundles without an images section continue to work unchanged.
func (c *Component) stageAppImages() error {
	if c.manifest == nil || c.manifest.Components.Images == nil {
		return nil
	}
	images := c.manifest.Components.Images.Images
	if len(images) == 0 {
		return nil
	}

	if err := os.MkdirAll(rke2ImageDir, 0755); err != nil {
		return err
	}

	for _, img := range images {
		srcPath := filepath.Join(c.extractDir, img.Path)
		// Reject path traversal — img.Path is trusted because the
		// builder wrote it, but defense-in-depth is cheap here.
		if !strings.HasPrefix(filepath.Clean(srcPath), c.extractDir) {
			return fmt.Errorf("invalid image path %q escapes bundle root", img.Path)
		}
		dstPath := filepath.Join(rke2ImageDir, filepath.Base(img.Path))

		log.Printf("  staging image tarball %s", filepath.Base(img.Path))
		src, err := os.Open(srcPath)
		if err != nil {
			return fmt.Errorf("opening %s: %w", img.Path, err)
		}
		dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
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

// installOnrampKubeconfig copies rke2.yaml into the onramp deployment
// user's ~/.kube/config so helm and kubectl invoked over ansible SSH
// (as that user) find a valid kubeconfig by default. aether-onramp's
// k8s role normally does this after its own RKE2 install; bootstrap
// short-circuits that role and must replicate the setup itself.
func (c *Component) installOnrampKubeconfig() error {
	onrampUser := "aether"
	if c.manifest != nil && c.manifest.Components.AetherOps != nil &&
		c.manifest.Components.AetherOps.OnrampUser != "" {
		onrampUser = c.manifest.Components.AetherOps.OnrampUser
	}

	u, err := userLookup(onrampUser)
	if err != nil {
		log.Printf("  onramp user %s not found; skipping kubeconfig install", onrampUser)
		return nil
	}

	src := filepath.Join(rke2ConfigDir, "rke2.yaml")
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}

	kubeDir := filepath.Join(u.HomeDir, ".kube")
	if err := os.MkdirAll(kubeDir, 0700); err != nil {
		return fmt.Errorf("mkdir %s: %w", kubeDir, err)
	}
	dst := filepath.Join(kubeDir, "config")
	if err := os.WriteFile(dst, data, 0600); err != nil {
		return fmt.Errorf("write %s: %w", dst, err)
	}
	if err := chownPath(kubeDir, u); err != nil {
		return err
	}
	if err := chownPath(dst, u); err != nil {
		return err
	}
	log.Printf("  installed %s (owner %s)", dst, onrampUser)
	return nil
}
