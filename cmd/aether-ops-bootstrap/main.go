package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/aether-gui/aether-ops-bootstrap/internal/launcher"
	"github.com/aether-gui/aether-ops-bootstrap/internal/state"
)

var version = "dev"

var commands = map[string]string{
	"install": "Full bootstrap from scratch",
	"upgrade": "Compare bundle manifest to state and apply deltas",
	"repair":  "Re-run all reconciliation steps regardless of state",
	"check":   "Preflight and plan only, no changes (dry-run)",
	"state":   "Print the current state file",
	"version": "Print version information",
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(0)
	}

	switch os.Args[1] {
	case "install":
		cmdRun("install", false, false)
	case "upgrade":
		cmdRun("upgrade", false, false)
	case "repair":
		cmdRun("repair", false, true)
	case "check":
		cmdRun("check", true, false)
	case "state":
		cmdState()
	case "version":
		cmdVersion()
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func cmdRun(action string, dryRun, repair bool) {
	bundlePath := ""
	force := false
	rolesCSV := ""

	for i := 2; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--bundle":
			if i+1 < len(os.Args) {
				bundlePath = os.Args[i+1]
				i++
			} else {
				log.Fatal("--bundle requires a path argument")
			}
		case "--force":
			force = true
		case "--roles":
			if i+1 < len(os.Args) {
				rolesCSV = os.Args[i+1]
				i++
			} else {
				log.Fatal("--roles requires a comma-separated list (e.g., mgmt,core)")
			}
		default:
			log.Fatalf("unknown flag: %s", os.Args[i])
		}
	}

	if bundlePath == "" {
		log.Fatalf("--bundle is required for %s", action)
	}

	// upgrade and repair always allow re-running on existing state.
	if action == "upgrade" || action == "repair" {
		force = true
	}

	var roles []launcher.Role
	if rolesCSV != "" {
		var err error
		roles, err = launcher.ParseRoles(rolesCSV)
		if err != nil {
			log.Fatalf("invalid --roles: %v", err)
		}
	}

	opts := launcher.InstallOpts{
		BundlePath: bundlePath,
		Force:      force,
		DryRun:     dryRun,
		Repair:     repair,
		Action:     action,
		Version:    version,
		Roles:      roles,
	}

	if err := launcher.Install(context.Background(), opts); err != nil {
		log.Fatalf("%s failed: %v", action, err)
	}
}

func cmdVersion() {
	fmt.Printf("aether-ops-bootstrap %s\n", version)

	// If state exists, show installed bundle version.
	st, err := state.Read(state.DefaultPath)
	if err == nil && st.BundleVersion != "" {
		fmt.Printf("installed bundle: %s\n", st.BundleVersion)
	}
}

func cmdState() {
	st, err := state.Read(state.DefaultPath)
	if err != nil {
		log.Fatalf("reading state: %v", err)
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		log.Fatalf("marshaling state: %v", err)
	}
	fmt.Println(string(data))
}

func usage() {
	fmt.Println("Usage: aether-ops-bootstrap <command> [flags]")
	fmt.Println()
	fmt.Println("Commands:")
	for _, name := range []string{"install", "upgrade", "repair", "check", "state", "version"} {
		fmt.Printf("  %-10s %s\n", name, commands[name])
	}
	fmt.Println()
	fmt.Println("Flags:")
	fmt.Println("  --bundle <path>    Path to the bundle tar.zst file (required)")
	fmt.Println("  --force            Override a prior successful install")
	fmt.Println("  --roles <roles>    Comma-separated node roles (default: all)")
	fmt.Println()
	fmt.Println("Roles:")
	fmt.Println("  mgmt       Management plane (aether-ops, Ansible)")
	fmt.Println("             Aliases: management")
	fmt.Println("  core       SD-Core control plane (RKE2, Helm)")
	fmt.Println("             Aliases: sd-core")
	fmt.Println("  ran        Radio access network")
	fmt.Println("             Aliases: srs-ran, ocudu")
	fmt.Println()
	fmt.Println("If --roles is omitted, all components are installed (single-node mode).")
	fmt.Println("Multiple roles can be combined: --roles mgmt,core")
}
