package launcher

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"errors"

	"github.com/aether-gui/aether-ops-bootstrap/internal/builder"
	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components"
	"github.com/aether-gui/aether-ops-bootstrap/internal/state"
)

// InstallOpts configures the install command.
type InstallOpts struct {
	BundlePath string
	Force      bool   // override prior successful install
	DryRun     bool   // plan only, don't apply
	Repair     bool   // re-apply all components regardless of state
	Action     string // "install", "upgrade", "repair", "check" — recorded in history
	Version    string
}

// Install runs the full bootstrap sequence.
func Install(ctx context.Context, opts InstallOpts) error {
	// Preflight checks.
	log.Println("running preflight checks...")
	if err := Preflight(); err != nil {
		return fmt.Errorf("preflight: %w", err)
	}

	// Load or initialize state.
	// DryRun (check), upgrade, and repair always allow existing state.
	allowExisting := opts.Force || opts.DryRun || opts.Repair
	st, err := loadOrInitState(allowExisting)
	if err != nil {
		return err
	}

	// Extract bundle.
	log.Printf("extracting bundle %s...", opts.BundlePath)
	extractDir, err := os.MkdirTemp("", "aether-bootstrap-*")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(extractDir)

	if err := builder.Unarchive(opts.BundlePath, extractDir); err != nil {
		return fmt.Errorf("extracting bundle: %w", err)
	}

	// Read manifest.
	manifest, err := bundle.Read(filepath.Join(extractDir, "manifest.json"))
	if err != nil {
		return fmt.Errorf("reading manifest: %w", err)
	}
	log.Printf("bundle version %s (schema %d)", manifest.BundleVersion, manifest.SchemaVersion)

	// Detect host suite for deb filtering.
	suite, err := DetectSuite()
	if err != nil {
		return fmt.Errorf("detecting suite: %w", err)
	}
	log.Printf("detected host suite: %s", suite)

	// Build component registry.
	registry := BuildRegistry(extractDir, manifest, suite)

	// Walk components: Plan → Apply.
	for _, comp := range registry.All() {
		desired := comp.DesiredVersion(manifest)
		current := comp.CurrentVersion(st)

		// In repair mode, pretend nothing is installed so all components re-apply.
		if opts.Repair {
			current = ""
		}

		plan, err := comp.Plan(current, desired)
		if errors.Is(err, components.ErrNotImplemented) {
			log.Printf("[%s] skipping (not yet implemented)", comp.Name())
			continue
		}
		if err != nil {
			return fmt.Errorf("planning %s: %w", comp.Name(), err)
		}

		if plan.NoOp {
			log.Printf("[%s] up to date (%s)", comp.Name(), current)
			continue
		}

		if opts.DryRun {
			log.Printf("[%s] would apply (%s → %s, %d actions)", comp.Name(), current, desired, len(plan.Actions))
			for _, a := range plan.Actions {
				log.Printf("  - %s", a.Description)
			}
			continue
		}

		log.Printf("[%s] applying (%s → %s)...", comp.Name(), current, desired)
		if err := comp.Apply(ctx, plan); err != nil {
			// Save partial state before returning the error.
			_ = writeState(st, opts.Version, manifest) // best-effort save on failure
			return fmt.Errorf("applying %s: %w", comp.Name(), err)
		}

		// Update state for this component.
		if st.Components == nil {
			st.Components = make(map[string]state.ComponentState)
		}
		st.Components[comp.Name()] = state.ComponentState{
			Version:     desired,
			InstalledAt: time.Now().UTC(),
		}

		log.Printf("[%s] done", comp.Name())
	}

	// Write final state.
	action := opts.Action
	if action == "" {
		action = "install"
	}
	st.History = append(st.History, state.HistoryEntry{
		Action:          action,
		Timestamp:       time.Now().UTC(),
		LauncherVersion: opts.Version,
		BundleVersion:   manifest.BundleVersion,
	})
	if err := writeState(st, opts.Version, manifest); err != nil {
		return err
	}

	if !opts.DryRun {
		log.Println("")
		log.Println("========================================")
		log.Println("  Bootstrap complete!")
		log.Println("========================================")
		log.Println("")
		log.Println("  aether-ops is running at http://127.0.0.1:8186")
		log.Println("")
		log.Println("  The default onramp user credentials are:")
		log.Println("    user: aether")
		log.Println("    pass: <as configured>")
		log.Println("")
		log.Println("  Change the default password immediately.")
		log.Println("")
	}

	return nil
}

func loadOrInitState(force bool) (*state.State, error) {
	st, err := state.Read(state.DefaultPath)
	if err == nil {
		// State file exists with a prior install.
		if !force {
			// Check if all components are installed.
			if len(st.Components) > 0 {
				return nil, fmt.Errorf("prior install detected (use --force to override); state at %s", state.DefaultPath)
			}
		}
		return st, nil
	}

	// No state file or read error — start fresh.
	return &state.State{
		SchemaVersion: state.SchemaVersion,
		Components:    make(map[string]state.ComponentState),
	}, nil
}

func writeState(st *state.State, launcherVersion string, manifest *bundle.Manifest) error {
	st.LauncherVersion = launcherVersion
	st.BundleVersion = manifest.BundleVersion
	st.BundleHash = manifest.BundleSHA256
	return state.Write(state.DefaultPath, st)
}
