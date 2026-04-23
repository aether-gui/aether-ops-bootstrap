package serviceaccount

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"os/user"
	"strings"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
	"github.com/aether-gui/aether-ops-bootstrap/internal/cmdutil"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components"
	"github.com/aether-gui/aether-ops-bootstrap/internal/state"
)

// Component creates the aether-ops account that serves as both the
// daemon's service user and the ansible SSH target for onramp
// playbook execution.
type Component struct {
	manifest *bundle.Manifest
}

func New() *Component {
	return &Component{}
}

// SetManifest allows the launcher to pass the manifest for reading
// onramp user configuration.
func (c *Component) SetManifest(m *bundle.Manifest) {
	c.manifest = m
}

func (c *Component) Name() string { return "service_account" }

func (c *Component) DesiredVersion(b *bundle.Manifest) string {
	return b.BundleVersion
}

func (c *Component) CurrentVersion(s *state.State) string {
	if cs, ok := s.Components["service_account"]; ok {
		return cs.Version
	}
	return ""
}

func (c *Component) Plan(current, desired string) (components.Plan, error) {
	if current == desired && desired != "" {
		return components.Plan{NoOp: true}, nil
	}

	// aether-ops is both the service account and the ansible target
	// user — a single login-capable account that runs the daemon AND
	// receives the ansible ssh sessions it generates. The password
	// defaults to "aether" for first-boot ansible access; operators
	// should change it after setup.
	password := "aether"
	if c.manifest != nil && c.manifest.Components.AetherOps != nil {
		if c.manifest.Components.AetherOps.OnrampPassword != "" {
			password = c.manifest.Components.AetherOps.OnrampPassword
		}
	}

	actions := []components.Action{
		{
			Description: "create aether-ops service + onramp account",
			Fn: func(ctx context.Context) error {
				return createServiceAccount(ctx, password)
			},
		},
	}

	return components.Plan{Actions: actions}, nil
}

func (c *Component) Apply(ctx context.Context, plan components.Plan) error {
	return components.ApplyPlan(ctx, c.Name(), plan)
}

func createServiceAccount(ctx context.Context, password string) error {
	if _, err := user.LookupGroup("aether-ops"); err != nil {
		cmd := exec.CommandContext(ctx, "groupadd", "aether-ops")
		if output, err := cmdutil.Run(ctx, cmd); err != nil {
			return fmt.Errorf("groupadd aether-ops: %w\n%s", err, output)
		}
		log.Printf("  created group aether-ops")
	}

	_, err := user.Lookup("aether-ops")
	userExists := err == nil

	if !userExists {
		// aether-ops needs a home directory and a real shell because
		// ansible ssh's into it to run onramp playbook tasks. The
		// password lets the daemon-generated hosts.ini (ansible_user=
		// aether-ops ansible_password=...) authenticate without key
		// provisioning on the localhost loop-back.
		cmd := exec.CommandContext(ctx, "useradd",
			"--create-home",
			"--shell", "/bin/bash",
			"--gid", "aether-ops",
			"aether-ops",
		)
		if output, err := cmdutil.Run(ctx, cmd); err != nil {
			return fmt.Errorf("useradd aether-ops: %w\n%s", err, output)
		}
		log.Printf("  created user aether-ops")

		// Only set password on initial creation — don't reset on upgrades.
		cmd = exec.CommandContext(ctx, "chpasswd")
		cmd.Stdin = strings.NewReader("aether-ops:" + password + "\n")
		if output, err := cmdutil.Run(ctx, cmd); err != nil {
			return fmt.Errorf("chpasswd for aether-ops: %w\n%s", err, output)
		}
		log.Printf("  set password for aether-ops")
	} else {
		log.Printf("  user aether-ops already exists (password unchanged)")
	}

	return nil
}
