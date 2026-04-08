package main

import (
	"fmt"
	"os"
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
		fmt.Println("install: not implemented")
	case "upgrade":
		fmt.Println("upgrade: not implemented")
	case "repair":
		fmt.Println("repair: not implemented")
	case "check":
		fmt.Println("check: not implemented")
	case "state":
		fmt.Println("state: not implemented")
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

func usage() {
	fmt.Println("Usage: aether-ops-bootstrap <command>")
	fmt.Println()
	fmt.Println("Commands:")
	// Print in a stable order matching the design doc.
	for _, name := range []string{"install", "upgrade", "repair", "check", "state", "version"} {
		fmt.Printf("  %-10s %s\n", name, commands[name])
	}
}
