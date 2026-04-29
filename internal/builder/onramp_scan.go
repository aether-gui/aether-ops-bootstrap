package builder

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
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
	aptRepoModuleKeys = map[string]bool{
		"copy":                           true,
		"ansible.builtin.copy":           true,
		"apt_repository":                 true,
		"ansible.builtin.apt_repository": true,
	}
	commandSplitPattern = regexp.MustCompile(`&&|\|\||;|\r?\n`)
)

// OnrampDependencyScan is the subset of downstream runtime dependencies
// bootstrap can auto-discover from the vendored aether-onramp tree today.
type OnrampDependencyScan struct {
	AptPackages     []string
	AptSources      map[string][]string
	AptRepositories []bundle.AptSourceSpec
	AptRepoSources  map[string][]string
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
		aptSources:      map[string]map[string]bool{},
		aptRepos:        map[string]bundle.AptSourceSpec{},
		aptRepoSources:  map[string]map[string]bool{},
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
		AptSources:      sortedSourceMap(acc.aptSources),
		AptRepositories: sortedAptRepos(acc.aptRepos),
		AptRepoSources:  sortedSourceMap(acc.aptRepoSources),
		PipRequirements: sortedKeys(acc.pipRequirements),
		Unresolved:      sortedKeys(acc.unresolved),
	}, nil
}

type scanAccumulator struct {
	aptPackages     map[string]bool
	aptSources      map[string]map[string]bool
	aptRepos        map[string]bundle.AptSourceSpec
	aptRepoSources  map[string]map[string]bool
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
		discoverAptRepository(n, filePath, acc)
		if taskModule, taskVal, ok := detectTaskModule(n); ok {
			switch {
			case aptModuleKeys[taskModule]:
				scanAptTaskWithContext(n, taskVal, filePath, acc)
			case pipModuleKeys[taskModule]:
				scanPipTask(taskVal, filePath, repoRoot, acc)
			case shellModuleKeys[taskModule]:
				scanShellTask(taskVal, filePath, repoRoot, acc)
			}
		}
		for i := 0; i+1 < len(n.Content); i += 2 {
			val := n.Content[i+1]
			scanYAMLNode(val, filePath, repoRoot, acc)
		}
	}
}

func discoverAptRepository(taskNode *yaml.Node, filePath string, acc *scanAccumulator) {
	module, moduleVal, ok := detectTaskModule(taskNode)
	if !ok {
		return
	}
	switch module {
	case "copy", "ansible.builtin.copy":
		if src, ok := aptSourceFromCopyTask(moduleVal); ok {
			recordAptRepository(src, filePath, acc)
		}
	case "apt_repository", "ansible.builtin.apt_repository":
		if src, ok := aptSourceFromAptRepositoryTask(moduleVal); ok {
			recordAptRepository(src, filePath, acc)
		}
	}
}

func aptSourceFromCopyTask(n *yaml.Node) (bundle.AptSourceSpec, bool) {
	if n == nil || n.Kind != yaml.MappingNode {
		return bundle.AptSourceSpec{}, false
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		key := n.Content[i]
		val := n.Content[i+1]
		if key.Kind != yaml.ScalarNode || key.Value != "content" || val.Kind != yaml.ScalarNode {
			continue
		}
		return parseDeb822Source(val.Value)
	}
	return bundle.AptSourceSpec{}, false
}

func aptSourceFromAptRepositoryTask(n *yaml.Node) (bundle.AptSourceSpec, bool) {
	if n == nil || n.Kind != yaml.MappingNode {
		return bundle.AptSourceSpec{}, false
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		key := n.Content[i]
		val := n.Content[i+1]
		if key.Kind != yaml.ScalarNode || val.Kind != yaml.ScalarNode {
			continue
		}
		if key.Value != "repo" {
			continue
		}
		fields := strings.Fields(strings.TrimSpace(val.Value))
		if len(fields) < 4 || fields[0] != "deb" {
			continue
		}
		repoURL := ""
		var comps []string
		for _, field := range fields[1:] {
			if strings.HasPrefix(field, "[") {
				continue
			}
			if strings.HasPrefix(field, "http://") || strings.HasPrefix(field, "https://") {
				repoURL = field
				continue
			}
			if repoURL != "" {
				comps = append(comps, field)
			}
		}
		if repoURL == "" || len(comps) == 0 {
			continue
		}
		return buildAptSourceSpec(repoURL, comps)
	}
	return bundle.AptSourceSpec{}, false
}

func parseDeb822Source(content string) (bundle.AptSourceSpec, bool) {
	var repoURL string
	var comps []string
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case strings.HasPrefix(line, "URIs:"):
			repoURL = strings.TrimSpace(strings.TrimPrefix(line, "URIs:"))
		case strings.HasPrefix(line, "Components:"):
			comps = strings.Fields(strings.TrimSpace(strings.TrimPrefix(line, "Components:")))
		}
	}
	if repoURL == "" || len(comps) == 0 {
		return bundle.AptSourceSpec{}, false
	}
	return buildAptSourceSpec(repoURL, comps)
}

func buildAptSourceSpec(repoURL string, comps []string) (bundle.AptSourceSpec, bool) {
	u, err := url.Parse(strings.TrimSpace(repoURL))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return bundle.AptSourceSpec{}, false
	}
	name := aptSourceName(u)
	return bundle.AptSourceSpec{
		Name:       name,
		URL:        strings.TrimRight(repoURL, "/"),
		Components: comps,
	}, true
}

func aptSourceName(u *url.URL) string {
	host := u.Hostname()
	path := strings.Trim(u.Path, "/")
	if host == "download.docker.com" && path == "linux/ubuntu" {
		return "docker"
	}
	name := host
	if path != "" {
		name += "-" + strings.NewReplacer("/", "-", ".", "-").Replace(path)
	}
	return name
}

func recordAptRepository(src bundle.AptSourceSpec, filePath string, acc *scanAccumulator) {
	if src.Name == "" || src.URL == "" || len(src.Components) == 0 {
		return
	}
	if existing, ok := acc.aptRepos[src.Name]; ok {
		if existing.URL == src.URL && strings.Join(existing.Components, ",") == strings.Join(src.Components, ",") {
			recordSource(src.Name, filePath, acc.aptRepoSources)
			return
		}
	}
	acc.aptRepos[src.Name] = src
	recordSource(src.Name, filePath, acc.aptRepoSources)
}

func detectTaskModule(n *yaml.Node) (module string, moduleVal *yaml.Node, ok bool) {
	if n == nil || n.Kind != yaml.MappingNode {
		return "", nil, false
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		key := n.Content[i]
		val := n.Content[i+1]
		if key.Kind != yaml.ScalarNode {
			continue
		}
		switch {
		case aptModuleKeys[key.Value], pipModuleKeys[key.Value], shellModuleKeys[key.Value], aptRepoModuleKeys[key.Value]:
			return key.Value, val, true
		}
	}
	return "", nil, false
}

func scanAptTaskWithContext(taskNode, aptNode *yaml.Node, filePath string, acc *scanAccumulator) {
	loopItems, hasStaticLoop, loopDynamic := extractStaticLoopItems(taskNode)
	if loopDynamic {
		acc.unresolved[fmt.Sprintf("%s: dynamic apt loop items", filePath)] = true
	}

	if aptNode == nil {
		return
	}
	if aptTaskRemovesPackages(aptNode) {
		return
	}
	switch aptNode.Kind {
	case yaml.ScalarNode:
		for _, pkg := range splitInlineNames(aptNode.Value) {
			recordLoopAwareValue(pkg, filePath, "apt package", loopItems, hasStaticLoop, acc.aptPackages, acc.aptSources, acc.unresolved)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(aptNode.Content); i += 2 {
			key := aptNode.Content[i]
			val := aptNode.Content[i+1]
			if key.Kind != yaml.ScalarNode {
				continue
			}
			if key.Value != "name" && key.Value != "pkg" && key.Value != "package" {
				continue
			}
			collectNamesNodeWithLoop(val, filePath, "apt package", loopItems, hasStaticLoop, acc.aptPackages, acc.aptSources, acc.unresolved)
		}
	}
}

func aptTaskRemovesPackages(n *yaml.Node) bool {
	if n == nil || n.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		key := n.Content[i]
		val := n.Content[i+1]
		if key.Kind != yaml.ScalarNode || val.Kind != yaml.ScalarNode {
			continue
		}
		if key.Value != "state" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(val.Value)) {
		case "absent", "removed", "purged":
			return true
		}
	}
	return false
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
	collectNamesNodeWithLoop(n, filePath, kind, nil, false, out, nil, unresolved)
}

func collectNamesNodeWithLoop(n *yaml.Node, filePath, kind string, loopItems []string, hasStaticLoop bool, out map[string]bool, sources map[string]map[string]bool, unresolved map[string]bool) {
	if n == nil {
		return
	}
	switch n.Kind {
	case yaml.ScalarNode:
		for _, item := range splitInlineNames(n.Value) {
			recordLoopAwareValue(item, filePath, kind, loopItems, hasStaticLoop, out, sources, unresolved)
		}
	case yaml.SequenceNode:
		for _, c := range n.Content {
			if c.Kind != yaml.ScalarNode {
				unresolved[fmt.Sprintf("%s: non-scalar %s entry", filePath, kind)] = true
				continue
			}
			recordLoopAwareValue(c.Value, filePath, kind, loopItems, hasStaticLoop, out, sources, unresolved)
		}
	default:
		unresolved[fmt.Sprintf("%s: unsupported %s value kind %d", filePath, kind, n.Kind)] = true
	}
}

func recordStaticValue(raw, filePath, kind string, out map[string]bool, sources map[string]map[string]bool, unresolved map[string]bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return
	}
	if isDynamicValue(value) {
		unresolved[fmt.Sprintf("%s: dynamic %s %q", filePath, kind, value)] = true
		return
	}
	out[value] = true
	recordSource(value, filePath, sources)
}

func recordLoopAwareValue(raw, filePath, kind string, loopItems []string, hasStaticLoop bool, out map[string]bool, sources map[string]map[string]bool, unresolved map[string]bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return
	}
	if !isLoopItemReference(value) {
		recordStaticValue(value, filePath, kind, out, sources, unresolved)
		return
	}
	if !hasStaticLoop {
		return
	}
	for _, item := range loopItems {
		recordStaticValue(item, filePath, kind, out, sources, unresolved)
	}
}

func recordSource(name, filePath string, sources map[string]map[string]bool) {
	if sources == nil || name == "" {
		return
	}
	if sources[name] == nil {
		sources[name] = map[string]bool{}
	}
	sources[name][filePath] = true
}

func extractStaticLoopItems(taskNode *yaml.Node) (items []string, hasStatic bool, dynamic bool) {
	if taskNode == nil || taskNode.Kind != yaml.MappingNode {
		return nil, false, false
	}
	for i := 0; i+1 < len(taskNode.Content); i += 2 {
		key := taskNode.Content[i]
		val := taskNode.Content[i+1]
		if key.Kind != yaml.ScalarNode {
			continue
		}
		switch key.Value {
		case "loop", "with_items":
			return scalarListFromNode(val)
		}
	}
	return nil, false, false
}

func scalarListFromNode(n *yaml.Node) (items []string, hasStatic bool, dynamic bool) {
	if n == nil {
		return nil, false, false
	}
	switch n.Kind {
	case yaml.SequenceNode:
		out := make([]string, 0, len(n.Content))
		for _, c := range n.Content {
			if c.Kind != yaml.ScalarNode {
				return nil, false, true
			}
			v := strings.TrimSpace(c.Value)
			if v == "" {
				continue
			}
			if isDynamicValue(v) {
				return nil, false, true
			}
			out = append(out, v)
		}
		return out, true, false
	case yaml.ScalarNode:
		v := strings.TrimSpace(n.Value)
		if v == "" {
			return nil, false, false
		}
		if isDynamicValue(v) {
			return nil, false, true
		}
		return splitInlineNames(v), true, false
	default:
		return nil, false, true
	}
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

func sortedSourceMap(in map[string]map[string]bool) map[string][]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string][]string, len(in))
	for name, refs := range in {
		out[name] = sortedKeys(refs)
	}
	return out
}

func sortedAptRepos(in map[string]bundle.AptSourceSpec) []bundle.AptSourceSpec {
	if len(in) == 0 {
		return nil
	}
	names := make([]string, 0, len(in))
	for name := range in {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]bundle.AptSourceSpec, 0, len(names))
	for _, name := range names {
		out = append(out, in[name])
	}
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

func isLoopItemReference(v string) bool {
	v = strings.TrimSpace(v)
	switch v {
	case "{{ item }}", "{{item}}":
		return true
	default:
		return false
	}
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
