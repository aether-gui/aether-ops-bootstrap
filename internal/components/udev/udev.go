// Package udev installs udev rule drop-ins from the bundle templates and
// reloads udev so the new rules take effect without a reboot.
//
// Rationale: USRP USB devices (B200/B210, USRP1, B100) bind under
// /dev/bus/usb with default 0660 root:root permissions, which blocks
// non-root processes — notably the aether/onramp service account that
// runs srsRAN against these radios — from opening the device. The
// vendor-supplied uhd-usrp.rules ships in templates/udev.rules.d and
// relaxes those nodes to 0666. Investigating the underlying root cause
// (likely a missing plugdev membership or a kernel/UHD packaging gap)
// is tracked separately; this component is the operational workaround.
package udev

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components"
	"github.com/aether-gui/aether-ops-bootstrap/internal/state"
)

const (
	rulesDir    = "/etc/udev/rules.d"
	templateDir = "udev.rules.d"
)

// Component installs every file under templates/udev.rules.d/ into
// /etc/udev/rules.d and reloads udev.
type Component struct {
	extractDir string
}

func New(extractDir string) *Component {
	return &Component{extractDir: extractDir}
}

func (c *Component) Name() string { return "udev" }

func (c *Component) DesiredVersion(b *bundle.Manifest) string {
	return b.BundleVersion
}

func (c *Component) CurrentVersion(s *state.State) string {
	if cs, ok := s.Components["udev"]; ok {
		return cs.Version
	}
	return ""
}

func (c *Component) Plan(current, desired string) (components.Plan, error) {
	if current == desired && desired != "" {
		return components.Plan{NoOp: true}, nil
	}

	srcDir := filepath.Join(c.extractDir, "templates", templateDir)
	entries, err := os.ReadDir(srcDir)
	if os.IsNotExist(err) {
		return components.Plan{NoOp: true}, nil
	}
	if err != nil {
		return components.Plan{}, fmt.Errorf("reading udev rule templates: %w", err)
	}

	var rules []string
	for _, e := range entries {
		if e.IsDir() || e.Name() == ".gitkeep" {
			continue
		}
		rules = append(rules, e.Name())
	}
	if len(rules) == 0 {
		return components.Plan{NoOp: true}, nil
	}

	actions := []components.Action{
		{
			Description: fmt.Sprintf("install %d udev rule file(s)", len(rules)),
			Fn: func(ctx context.Context) error {
				if err := os.MkdirAll(rulesDir, 0755); err != nil {
					return err
				}
				for _, name := range rules {
					data, err := os.ReadFile(filepath.Join(srcDir, name))
					if err != nil {
						return fmt.Errorf("reading udev rule %s: %w", name, err)
					}
					destPath := filepath.Join(rulesDir, name)
					if err := os.WriteFile(destPath, data, 0644); err != nil {
						return fmt.Errorf("writing %s: %w", destPath, err)
					}
					log.Printf("  wrote %s", destPath)
				}
				return nil
			},
		},
		{
			Description: "reload and trigger udev",
			Fn: func(ctx context.Context) error {
				reload := exec.CommandContext(ctx, "udevadm", "control", "--reload-rules")
				if out, err := reload.CombinedOutput(); err != nil {
					return fmt.Errorf("udevadm control --reload-rules: %w\n%s", err, out)
				}
				trigger := exec.CommandContext(ctx, "udevadm", "trigger")
				if out, err := trigger.CombinedOutput(); err != nil {
					return fmt.Errorf("udevadm trigger: %w\n%s", err, out)
				}
				log.Printf("  reloaded udev rules and triggered devices")
				return nil
			},
		},
	}

	return components.Plan{Actions: actions}, nil
}

func (c *Component) Apply(ctx context.Context, plan components.Plan) error {
	return components.ApplyPlan(ctx, c.Name(), plan)
}
