// Package diagnostics collects system state into a tar.gz bundle for
// remote troubleshooting. Individual collector errors are accumulated in
// collection-errors.txt inside the bundle but never abort the overall
// collection process.
package diagnostics

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/aether-gui/aether-ops-bootstrap/internal/state"
)

// CollectOpts configures what the collector gathers.
type CollectOpts struct {
	LogFile   string // path to the bootstrap log file (may be empty)
	StateFile string // defaults to state.DefaultPath if empty
	Version   string // launcher version to record in metadata
}

// diagMeta is written as diag-meta.json at the bundle root.
type diagMeta struct {
	Timestamp string `json:"timestamp"`
	Hostname  string `json:"hostname"`
	Version   string `json:"version"`
}

// Collect gathers diagnostic info and writes a tar.gz to outputDir.
// Returns the path to the created tarball. Individual collector failures
// are accumulated in collection-errors.txt but never stop collection.
func Collect(outputDir string, opts CollectOpts) (string, error) {
	if opts.StateFile == "" {
		opts.StateFile = state.DefaultPath
	}

	stagingDir, err := os.MkdirTemp("", "aether-diag-*")
	if err != nil {
		return "", fmt.Errorf("creating staging dir: %w", err)
	}
	defer os.RemoveAll(stagingDir)

	var collectionErrors []string
	record := func(name string, err error) {
		if err != nil {
			msg := fmt.Sprintf("[%s] %v", name, err)
			collectionErrors = append(collectionErrors, msg)
			log.Printf("diagnostic collector: %s", msg)
		}
	}

	// Run all collectors.
	record("system", collectSystemInfo(stagingDir))
	record("state", collectBootstrapState(stagingDir, opts))
	record("services", collectServices(stagingDir))
	record("kubernetes", collectKubernetes(stagingDir))
	record("configs", collectConfigs(stagingDir))
	record("manifest", collectManifest(stagingDir))

	// Write collection errors if any.
	if len(collectionErrors) > 0 {
		data := []byte(strings.Join(collectionErrors, "\n") + "\n")
		_ = writeFile(stagingDir, "collection-errors.txt", data)
	}

	// Write metadata.
	hostname, _ := os.Hostname()
	meta := diagMeta{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Hostname:  hostname,
		Version:   opts.Version,
	}
	metaJSON, _ := json.MarshalIndent(meta, "", "  ")
	_ = writeFile(stagingDir, "diag-meta.json", metaJSON)

	// Create the tarball.
	ts := time.Now().Format("20060102T150405")
	tarballName := fmt.Sprintf("aether-bootstrap-diag-%s.tar.gz", ts)
	tarballPath := filepath.Join(outputDir, tarballName)
	if err := createTarGz(stagingDir, tarballPath); err != nil {
		return "", fmt.Errorf("creating tarball: %w", err)
	}

	return tarballPath, nil
}

// --- Sub-collectors ---

func collectSystemInfo(stagingDir string) error {
	dir := filepath.Join(stagingDir, "system")

	copyFileIfExists("/etc/os-release", dir, "os-release")

	commands := map[string][]string{
		"uname.txt":    {"uname", "-a"},
		"cpu.txt":      {"lscpu"},
		"nproc.txt":    {"nproc"},
		"memory.txt":   {"free", "-h"},
		"disk.txt":     {"df", "-h"},
		"network.txt":  {"ip", "addr"},
		"hostname.txt": {"hostname"},
		"datetime.txt": {"date"},
	}

	for filename, cmd := range commands {
		out := runCommand(cmd[0], cmd[1:]...)
		_ = writeFile(dir, filename, []byte(out))
	}

	// Append timedatectl to datetime.
	tdOut := runCommand("timedatectl")
	if tdOut != "" {
		existing, _ := os.ReadFile(filepath.Join(dir, "datetime.txt"))
		_ = writeFile(dir, "datetime.txt", []byte(string(existing)+"\n"+tdOut))
	}

	return nil
}

func collectBootstrapState(stagingDir string, opts CollectOpts) error {
	dir := filepath.Join(stagingDir, "state")

	copyFileIfExists(opts.StateFile, dir, "state.json")

	if opts.LogFile != "" {
		copyFileIfExists(opts.LogFile, dir, "bootstrap.log")
	}

	return nil
}

func collectServices(stagingDir string) error {
	dir := filepath.Join(stagingDir, "services")
	units := []string{"rke2-server", "aether-ops"}

	for _, unit := range units {
		status := runCommand("systemctl", "status", unit)
		_ = writeFile(dir, unit+"-status.txt", []byte(status))

		journal := runCommand("journalctl", "-u", unit, "--no-pager", "-n", "500")
		_ = writeFile(dir, unit+"-journal.txt", []byte(journal))
	}

	return nil
}

func collectKubernetes(stagingDir string) error {
	kubectlPath := "/var/lib/rancher/rke2/bin/kubectl"
	kubeconfigPath := "/etc/rancher/rke2/rke2.yaml"

	if _, err := os.Stat(kubectlPath); err != nil {
		return nil // kubectl not installed, skip silently
	}
	if _, err := os.Stat(kubeconfigPath); err != nil {
		return nil // no kubeconfig, skip silently
	}

	dir := filepath.Join(stagingDir, "kubernetes")
	kcFlag := "--kubeconfig=" + kubeconfigPath

	commands := map[string][]string{
		"nodes.txt":          {kubectlPath, kcFlag, "get", "nodes", "-o", "wide"},
		"pods.txt":           {kubectlPath, kcFlag, "get", "pods", "-A"},
		"describe-nodes.txt": {kubectlPath, kcFlag, "describe", "nodes"},
	}

	for filename, cmd := range commands {
		out := runCommand(cmd[0], cmd[1:]...)
		_ = writeFile(dir, filename, []byte(out))
	}

	return nil
}

// secretBasenames lists filenames that are excluded from the diagnostic
// bundle to avoid accidental credential exfiltration.
var secretBasenames = map[string]bool{
	"env":          true, // API tokens, passwords
	".env":         true,
	"secrets.yaml": true,
	"secrets.json": true,
	"token":        true,
	"password":     true,
}

func collectConfigs(stagingDir string) error {
	dir := filepath.Join(stagingDir, "configs")

	copyFileIfExists("/etc/rancher/rke2/config.yaml", dir, "rke2-config.yaml")
	copyFileIfExists("/etc/systemd/system/aether-ops.service", dir, "aether-ops.service")

	// Copy /etc/aether-ops/ excluding known secret files.
	copyDirIfExists("/etc/aether-ops", dir, "aether-ops-config")

	return nil
}

func collectManifest(stagingDir string) error {
	dir := filepath.Join(stagingDir, "bundle")

	// The launcher doesn't persist the manifest separately, but the state
	// dir may have it from a prior extract. Check the common location.
	manifestPath := filepath.Join(filepath.Dir(state.DefaultPath), "manifest.json")
	copyFileIfExists(manifestPath, dir, "manifest.json")

	return nil
}

// --- Helpers ---

// runCommand executes a command and returns combined stdout+stderr.
// On failure (including command not found), the error is included in the
// returned string so the caller can still write useful output.
func runCommand(name string, args ...string) string {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if len(out) > 0 {
			return string(out) + "\n[error: " + err.Error() + "]"
		}
		return "[error: " + err.Error() + "]"
	}
	return string(out)
}

// writeFile writes data to a file under baseDir, creating parent dirs.
func writeFile(baseDir, subpath string, data []byte) error {
	full := filepath.Join(baseDir, subpath)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		return err
	}
	return os.WriteFile(full, data, 0644)
}

// copyFileIfExists copies src to baseDir/subpath if src exists.
// Missing source is silently ignored.
func copyFileIfExists(src, baseDir, subpath string) {
	data, err := os.ReadFile(src)
	if err != nil {
		return
	}
	_ = writeFile(baseDir, subpath, data)
}

// copyDirIfExists copies regular files from srcDir into
// baseDir/subpath, preserving relative paths. Symlinks, special files,
// and files whose basenames appear in secretBasenames are skipped.
// Missing source directory is silently ignored.
func copyDirIfExists(srcDir, baseDir, subpath string) {
	info, err := os.Stat(srcDir)
	if err != nil || !info.IsDir() {
		return
	}
	_ = filepath.WalkDir(srcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		fi, err := d.Info()
		if err != nil || !fi.Mode().IsRegular() {
			return nil
		}
		if secretBasenames[filepath.Base(path)] {
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		_ = writeFile(filepath.Join(baseDir, subpath), rel, data)
		return nil
	})
}

// createTarGz creates a gzip-compressed tar archive from the contents
// of sourceDir. Uses plain gzip (not zstd) so operators can open
// bundles on any machine without special tools.
func createTarGz(sourceDir, outputPath string) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return err
	}

	outFile, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer outFile.Close()

	gw := gzip.NewWriter(outFile)
	tw := tar.NewWriter(gw)

	err = filepath.WalkDir(sourceDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(rel)

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		_, err = io.Copy(tw, f)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		return closeErr
	})

	if err != nil {
		return err
	}

	if err := tw.Close(); err != nil {
		return err
	}
	return gw.Close()
}
