package builder

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
)

// BuildWheelhouse downloads Python wheels for the provided requirements
// into the bundle staging area. The build intentionally rejects source
// distributions to keep offline installs from turning into ad hoc native
// build environments.
func BuildWheelhouse(ctx context.Context, requirements []string, stageDir string) (*bundle.WheelhouseEntry, error) {
	requirements = normalizeRequirements(requirements)
	if len(requirements) == 0 {
		return nil, nil
	}

	wheelDir := filepath.Join(stageDir, "wheelhouse")
	if err := os.MkdirAll(wheelDir, 0o755); err != nil {
		return nil, err
	}

	reqPath := filepath.Join(wheelDir, "requirements.txt")
	if err := os.WriteFile(reqPath, []byte(strings.Join(requirements, "\n")+"\n"), 0o644); err != nil {
		return nil, fmt.Errorf("writing wheelhouse requirements: %w", err)
	}

	cmd := exec.CommandContext(ctx,
		"python3", "-m", "pip", "download",
		"--dest", wheelDir,
		"--requirement", reqPath,
		"--only-binary=:all:",
		"--disable-pip-version-check",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("python3 -m pip download: %w\n%s", err, out)
	}

	files, err := hashTree(wheelDir, "wheelhouse")
	if err != nil {
		return nil, fmt.Errorf("hashing wheelhouse: %w", err)
	}

	return &bundle.WheelhouseEntry{
		Requirements: requirements,
		Files:        files,
	}, nil
}

func normalizeRequirements(requirements []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, req := range requirements {
		req = strings.TrimSpace(req)
		if req == "" || seen[req] {
			continue
		}
		seen[req] = true
		out = append(out, req)
	}
	sort.Strings(out)
	return out
}
