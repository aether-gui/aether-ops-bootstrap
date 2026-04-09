package serviceaccount

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"os/user"
	"strings"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components"
	"github.com/aether-gui/aether-ops-bootstrap/internal/state"
)

// Component creates the aether-ops service account and the onramp
// deployment user.
type Component struct{}

func New() *Component {
	return &Component{}
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

	actions := []components.Action{
		{
			Description: "create aether-ops service account",
			Fn: func(ctx context.Context) error {
				return createServiceAccount(ctx)
			},
		},
		{
			Description: "create onramp deployment user",
			Fn: func(ctx context.Context) error {
				return createOnrampUser(ctx, "aether", "aether")
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

func createServiceAccount(ctx context.Context) error {
	if _, err := user.LookupGroup("aether-ops"); err != nil {
		cmd := exec.CommandContext(ctx, "groupadd", "--system", "aether-ops")
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("groupadd aether-ops: %w\n%s", err, output)
		}
		log.Printf("  created group aether-ops")
	}

	if _, err := user.Lookup("aether-ops"); err != nil {
		cmd := exec.CommandContext(ctx, "useradd",
			"--system",
			"--no-create-home",
			"--shell", "/usr/sbin/nologin",
			"--gid", "aether-ops",
			"aether-ops",
		)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("useradd aether-ops: %w\n%s", err, output)
		}
		log.Printf("  created user aether-ops")
	}

	return nil
}

func createOnrampUser(ctx context.Context, username, password string) error {
	if _, err := user.Lookup(username); err != nil {
		cmd := exec.CommandContext(ctx, "useradd",
			"--create-home",
			"--shell", "/bin/bash",
			username,
		)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("useradd %s: %w\n%s", username, err, output)
		}
		log.Printf("  created user %s", username)
	}

	// Set password via chpasswd.
	cmd := exec.CommandContext(ctx, "chpasswd")
	cmd.Stdin = strings.NewReader(fmt.Sprintf("%s:%s\n", username, password))
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("chpasswd for %s: %w\n%s", username, err, output)
	}
	log.Printf("  set password for %s", username)
	return nil
}
