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
		cmdInstall(false)
	case "upgrade":
		fmt.Println("upgrade: not implemented")
	case "repair":
		fmt.Println("repair: not implemented")
	case "check":
		cmdInstall(true)
	case "state":
		cmdState()
	case "version":
		fmt.Printf("aether-ops-bootstrap %s\n", version)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func cmdInstall(dryRun bool) {
	bundlePath := ""
	force := false

	// Simple arg parsing for install subcommand.
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
		default:
			log.Fatalf("unknown flag: %s", os.Args[i])
		}
	}

	if bundlePath == "" {
		log.Fatal("--bundle is required for install")
	}

	opts := launcher.InstallOpts{
		BundlePath: bundlePath,
		Force:      force,
		DryRun:     dryRun,
		Version:    version,
	}

	if err := launcher.Install(context.Background(), opts); err != nil {
		log.Fatalf("install failed: %v", err)
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
	fmt.Println("Install flags:")
	fmt.Println("  --bundle <path>   Path to the bundle tar.zst file (required)")
	fmt.Println("  --force           Override a prior successful install")
}
