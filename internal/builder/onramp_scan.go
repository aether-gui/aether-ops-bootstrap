package builder

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	aptModuleKeys = map[string]bool{
		"apt":                     true,
		"ansible.builtin.apt":     true,
		"package":                 true,
		"ansible.builtin.package": true,
	}
	pipModuleKeys = map[string]bool{
		"pip":                 true,
		"ansible.builtin.pip": true,
	}
	shellModuleKeys = map[string]bool{
		"shell":                   true,
		"ansible.builtin.shell":   true,
		"command":                 true,
		"ansible.builtin.command": true,
	}
	commandSplitPattern = regexp.MustCompile(`&&|\|\||;|\r?\n`)
)

// OnrampDependencyScan is the subset of downstream runtime dependencies
// bootstrap can auto-discover from the vendored aether-onramp tree today.
type OnrampDependencyScan struct {
	AptPackages     []string
	PipRequirements []string
	Unresolved      []string
}

// ScanOnrampDependencies walks an aether-onramp checkout and extracts
// apt/package names plus pip requirements referenced in YAML task files.
// Dynamic references that cannot be resolved at bundle-build time are
// surfaced in Unresolved so callers can decide whether to fail the build.
func ScanOnrampDependencies(root string) (*OnrampDependencyScan, error) {
	acc := &scanAccumulator{
		aptPackages:     map[string]bool{},
		pipRequirements: map[string]bool{},
		unresolved:      map[string]bool{},
		seenReqFiles:    map[string]bool{},
	}

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !isYAMLFile(path) {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}

		var rootNode yaml.Node
		if err := yaml.Unmarshal(data, &rootNode); err != nil {
			// Some repos contain YAML-shaped documentation snippets that
			// are not valid task files. Skip those instead of failing the
			// whole build on a non-executable artifact.
			return nil
		}

		scanYAMLNode(&rootNode, path, root, acc)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &OnrampDependencyScan{
		AptPackages:     sortedKeys(acc.aptPackages),
		PipRequirements: sortedKeys(acc.pipRequirements),
		Unresolved:      sortedKeys(acc.unresolved),
	}, nil
}

type scanAccumulator struct {
	aptPackages     map[string]bool
	pipRequirements map[string]bool
	unresolved      map[string]bool
	seenReqFiles    map[string]bool
}

func scanYAMLNode(n *yaml.Node, filePath, repoRoot string, acc *scanAccumulator) {
	if n == nil {
		return
	}

	switch n.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, c := range n.Content {
			scanYAMLNode(c, filePath, repoRoot, acc)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := n.Content[i]
			val := n.Content[i+1]
			if key.Kind == yaml.ScalarNode {
				switch {
				case aptModuleKeys[key.Value]:
					scanAptTask(val, filePath, acc)
				case pipModuleKeys[key.Value]:
					scanPipTask(val, filePath, repoRoot, acc)
				case shellModuleKeys[key.Value]:
					scanShellTask(val, filePath, repoRoot, acc)
				}
			}
			scanYAMLNode(val, filePath, repoRoot, acc)
		}
	}
}

func scanAptTask(n *yaml.Node, filePath string, acc *scanAccumulator) {
	if n == nil {
		return
	}
	switch n.Kind {
	case yaml.ScalarNode:
		for _, pkg := range splitInlineNames(n.Value) {
			recordStaticValue(pkg, filePath, "apt package", acc.aptPackages, acc.unresolved)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := n.Content[i]
			val := n.Content[i+1]
			if key.Kind != yaml.ScalarNode {
				continue
			}
			if key.Value != "name" && key.Value != "pkg" && key.Value != "package" {
				continue
			}
			collectNamesNode(val, filePath, "apt package", acc.aptPackages, acc.unresolved)
		}
	}
}

func scanPipTask(n *yaml.Node, filePath, repoRoot string, acc *scanAccumulator) {
	if n == nil || n.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		key := n.Content[i]
		val := n.Content[i+1]
		if key.Kind != yaml.ScalarNode {
			continue
		}
		switch key.Value {
		case "name":
			collectNamesNode(val, filePath, "pip requirement", acc.pipRequirements, acc.unresolved)
		case "requirements":
			if val.Kind != yaml.ScalarNode {
				acc.unresolved[fmt.Sprintf("%s: dynamic pip requirements reference", filePath)] = true
				continue
			}
			reqPath := strings.TrimSpace(val.Value)
			if reqPath == "" {
				continue
			}
			if isDynamicValue(reqPath) {
				acc.unresolved[fmt.Sprintf("%s: dynamic pip requirements path %q", filePath, reqPath)] = true
				continue
			}
			if filepath.IsAbs(reqPath) {
				acc.unresolved[fmt.Sprintf("%s: absolute pip requirements path %q", filePath, reqPath)] = true
				continue
			}
			resolved := filepath.Clean(filepath.Join(filepath.Dir(filePath), reqPath))
			if !strings.HasPrefix(resolved, repoRoot+string(filepath.Separator)) && resolved != repoRoot {
				acc.unresolved[fmt.Sprintf("%s: pip requirements path escapes repo %q", filePath, reqPath)] = true
				continue
			}
			if err := collectRequirementsFile(resolved, repoRoot, acc); err != nil {
				acc.unresolved[fmt.Sprintf("%s: %v", filePath, err)] = true
			}
		}
	}
}

func scanShellTask(n *yaml.Node, filePath, repoRoot string, acc *scanAccumulator) {
	command := shellCommandText(n)
	if command == "" {
		return
	}

	for _, segment := range splitCommandSegments(command) {
		scanShellSegmentForPip(segment, filePath, repoRoot, acc)
	}
}

func shellCommandText(n *yaml.Node) string {
	if n == nil {
		return ""
	}
	switch n.Kind {
	case yaml.ScalarNode:
		return strings.TrimSpace(n.Value)
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := n.Content[i]
			val := n.Content[i+1]
			if key.Kind != yaml.ScalarNode || val.Kind != yaml.ScalarNode {
				continue
			}
			switch key.Value {
			case "cmd", "argv", "_raw_params":
				return strings.TrimSpace(val.Value)
			}
		}
	}
	return ""
}

func splitCommandSegments(command string) []string {
	raw := commandSplitPattern.Split(command, -1)
	out := make([]string, 0, len(raw))
	for _, seg := range raw {
		seg = strings.TrimSpace(seg)
		if seg != "" {
			out = append(out, seg)
		}
	}
	return out
}

func scanShellSegmentForPip(segment, filePath, repoRoot string, acc *scanAccumulator) {
	fields := strings.Fields(segment)
	if len(fields) == 0 {
		return
	}

	start := 0
	switch {
	case fields[0] == "pip" || strings.HasSuffix(fields[0], "/pip") || strings.HasSuffix(fields[0], "/pip3") || fields[0] == "pip3":
		start = 0
	case len(fields) >= 3 && (fields[0] == "python" || fields[0] == "python3" || strings.HasSuffix(fields[0], "/python") || strings.HasSuffix(fields[0], "/python3")) && fields[1] == "-m" && fields[2] == "pip":
		start = 2
	default:
		return
	}

	args := fields[start+1:]
	if len(args) == 0 {
		return
	}
	if args[0] != "install" {
		return
	}
	collectShellPipArgs(args[1:], filePath, repoRoot, acc)
}

func collectShellPipArgs(args []string, filePath, repoRoot string, acc *scanAccumulator) {
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			continue
		}
		if isDynamicValue(arg) {
			acc.unresolved[fmt.Sprintf("%s: dynamic shell pip argument %q", filePath, arg)] = true
			continue
		}
		switch {
		case strings.HasPrefix(arg, "-r"):
			ref := strings.TrimSpace(strings.TrimPrefix(arg, "-r"))
			if ref == "" {
				i++
				if i >= len(args) {
					acc.unresolved[fmt.Sprintf("%s: shell pip install missing requirements path", filePath)] = true
					break
				}
				ref = strings.TrimSpace(args[i])
			}
			if err := collectShellRequirementsRef(ref, filePath, repoRoot, acc); err != nil {
				acc.unresolved[fmt.Sprintf("%s: %v", filePath, err)] = true
			}
		case strings.HasPrefix(arg, "--requirement="):
			ref := strings.TrimSpace(strings.TrimPrefix(arg, "--requirement="))
			if err := collectShellRequirementsRef(ref, filePath, repoRoot, acc); err != nil {
				acc.unresolved[fmt.Sprintf("%s: %v", filePath, err)] = true
			}
		case arg == "--requirement":
			i++
			if i >= len(args) {
				acc.unresolved[fmt.Sprintf("%s: shell pip install missing requirements path", filePath)] = true
				break
			}
			if err := collectShellRequirementsRef(strings.TrimSpace(args[i]), filePath, repoRoot, acc); err != nil {
				acc.unresolved[fmt.Sprintf("%s: %v", filePath, err)] = true
			}
		case strings.HasPrefix(arg, "-"):
			// Ignore common installer flags. Source/index altering flags
			// should stay visible as unresolved for offline builds.
			if isSupportedShellPipFlag(arg) {
				continue
			}
			acc.unresolved[fmt.Sprintf("%s: unsupported shell pip flag %q", filePath, arg)] = true
		default:
			recordShellPipRequirement(arg, filePath, acc)
		}
	}
}

func collectShellRequirementsRef(ref, filePath, repoRoot string, acc *scanAccumulator) error {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return fmt.Errorf("shell pip install missing requirements path")
	}
	if isDynamicValue(ref) {
		return fmt.Errorf("dynamic shell pip requirements path %q", ref)
	}
	if filepath.IsAbs(ref) {
		return fmt.Errorf("absolute shell pip requirements path %q", ref)
	}
	resolved := filepath.Clean(filepath.Join(filepath.Dir(filePath), ref))
	if !strings.HasPrefix(resolved, repoRoot+string(filepath.Separator)) && resolved != repoRoot {
		return fmt.Errorf("shell pip requirements path escapes repo %q", ref)
	}
	return collectRequirementsFile(resolved, repoRoot, acc)
}

func isSupportedShellPipFlag(arg string) bool {
	switch {
	case arg == "--upgrade", arg == "-U", arg == "--user", arg == "--no-input", arg == "--disable-pip-version-check":
		return true
	default:
		return false
	}
}

func recordShellPipRequirement(arg, filePath string, acc *scanAccumulator) {
	switch {
	case strings.Contains(arg, "://"), strings.Contains(arg, "git+"), strings.HasPrefix(arg, "."):
		acc.unresolved[fmt.Sprintf("%s: unsupported shell pip requirement %q", filePath, arg)] = true
	default:
		acc.pipRequirements[arg] = true
	}
}

func collectNamesNode(n *yaml.Node, filePath, kind string, out, unresolved map[string]bool) {
	if n == nil {
		return
	}
	switch n.Kind {
	case yaml.ScalarNode:
		for _, item := range splitInlineNames(n.Value) {
			recordStaticValue(item, filePath, kind, out, unresolved)
		}
	case yaml.SequenceNode:
		for _, c := range n.Content {
			if c.Kind != yaml.ScalarNode {
				unresolved[fmt.Sprintf("%s: non-scalar %s entry", filePath, kind)] = true
				continue
			}
			recordStaticValue(c.Value, filePath, kind, out, unresolved)
		}
	default:
		unresolved[fmt.Sprintf("%s: unsupported %s value kind %d", filePath, kind, n.Kind)] = true
	}
}

func recordStaticValue(raw, filePath, kind string, out, unresolved map[string]bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return
	}
	if isDynamicValue(value) {
		unresolved[fmt.Sprintf("%s: dynamic %s %q", filePath, kind, value)] = true
		return
	}
	out[value] = true
}

func collectRequirementsFile(path, repoRoot string, acc *scanAccumulator) error {
	if acc.seenReqFiles[path] {
		return nil
	}
	acc.seenReqFiles[path] = true

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("reading pip requirements %q: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if idx := strings.Index(line, " #"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}
		if line == "" {
			continue
		}

		switch {
		case strings.HasPrefix(line, "-r "), strings.HasPrefix(line, "--requirement "):
			ref := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "-r "), "--requirement "))
			if ref == "" || isDynamicValue(ref) || filepath.IsAbs(ref) {
				acc.unresolved[fmt.Sprintf("%s: unsupported nested requirement %q", path, line)] = true
				continue
			}
			resolved := filepath.Clean(filepath.Join(filepath.Dir(path), ref))
			if !strings.HasPrefix(resolved, repoRoot+string(filepath.Separator)) && resolved != repoRoot {
				acc.unresolved[fmt.Sprintf("%s: nested requirement escapes repo %q", path, line)] = true
				continue
			}
			if err := collectRequirementsFile(resolved, repoRoot, acc); err != nil {
				acc.unresolved[fmt.Sprintf("%s: %v", path, err)] = true
			}
		case strings.HasPrefix(line, "-e "), strings.Contains(line, "git+"), strings.Contains(line, "://"):
			acc.unresolved[fmt.Sprintf("%s: unsupported requirement %q", path, line)] = true
		case strings.HasPrefix(line, "--"):
			// Preserve benign pip options in the generated wheelhouse
			// requirement set only when they do not alter indexes/sources.
			acc.unresolved[fmt.Sprintf("%s: unsupported pip option %q", path, line)] = true
		default:
			acc.pipRequirements[line] = true
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scanning pip requirements %q: %w", path, err)
	}
	return nil
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func splitInlineNames(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if strings.Contains(raw, ",") {
		parts := strings.Split(raw, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
		return out
	}
	return []string{raw}
}

func isDynamicValue(v string) bool {
	return strings.Contains(v, "{{") || strings.Contains(v, "{%")
}

func isYAMLFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		return true
	default:
		return false
	}
}
