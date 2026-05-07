package builder

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aether-gui/aether-ops-bootstrap/internal/deb"
)

// fixturePackage returns a *deb.Package with a hand-crafted RawStanza
// so the apt-repo writer has realistic input including Breaks /
// Conflicts / Replaces lines that must round-trip verbatim.
func fixturePackage(name, version, arch, originalFilename string, extra ...string) *deb.Package {
	var sb strings.Builder
	sb.WriteString("Package: " + name + "\n")
	sb.WriteString("Version: " + version + "\n")
	sb.WriteString("Architecture: " + arch + "\n")
	sb.WriteString("Filename: " + originalFilename + "\n")
	sb.WriteString("Size: 12345\n")
	sb.WriteString("SHA256: deadbeef\n")
	for _, line := range extra {
		sb.WriteString(line + "\n")
	}
	sb.WriteString("Description: fixture\n")
	return &deb.Package{
		Name:      name,
		Version:   version,
		Arch:      arch,
		Filename:  originalFilename,
		SHA256:    "deadbeef",
		Size:      12345,
		RawStanza: []byte(sb.String()),
	}
}

func TestBuildAptRepo_WritesPackagesAndRelease(t *testing.T) {
	stage := t.TempDir()
	codename := "noble"

	// Pre-create the pool layout the way FetchDebs would, so Packages
	// references actually point at real files (mirrors integration
	// path even though the repo writer doesn't read them).
	for _, p := range []struct{ arch, basename string }{
		{"amd64", "iptables-persistent_1.0.20_amd64.deb"},
		{"all", "ansible_9.2.0_all.deb"},
	} {
		dir := filepath.Join(stage, "apt-repo", "pool", codename, p.arch)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, p.basename), []byte("fake-deb"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	pkgs := []*deb.Package{
		fixturePackage("iptables-persistent", "1.0.20", "amd64",
			"pool/main/i/iptables-persistent/iptables-persistent_1.0.20_amd64.deb",
			"Breaks: ufw",
			"Conflicts: ufw",
			"Replaces: iptables-persistent (<< 1.0.20)"),
		fixturePackage("ansible", "9.2.0", "all",
			"pool/universe/a/ansible/ansible_9.2.0_all.deb"),
	}

	if err := BuildAptRepo(stage, pkgs, []string{codename}); err != nil {
		t.Fatalf("BuildAptRepo: %v", err)
	}

	// Per-arch Packages files exist with rewritten Filename: lines.
	for _, arch := range []string{"amd64", "all"} {
		path := filepath.Join(stage, "apt-repo", "dists", codename, "main", "binary-"+arch, "Packages")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("missing %s: %v", path, err)
		}
		got := string(data)
		// Filename rewrite: must point at pool/<codename>/<arch>/<basename>.
		wantFilenamePrefix := "Filename: pool/" + codename + "/" + arch + "/"
		if !strings.Contains(got, wantFilenamePrefix) {
			t.Errorf("%s missing rewritten Filename prefix %q\n--- content ---\n%s",
				path, wantFilenamePrefix, got)
		}
		// Original upstream Filename path must NOT survive.
		for _, oldPath := range []string{"pool/main/i/iptables-persistent/", "pool/universe/a/ansible/"} {
			if strings.Contains(got, oldPath) {
				t.Errorf("%s leaked upstream pool path %q", path, oldPath)
			}
		}
	}

	// Breaks/Conflicts/Replaces survive the round-trip.
	amdPackages, err := os.ReadFile(filepath.Join(stage, "apt-repo", "dists", codename,
		"main", "binary-amd64", "Packages"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Breaks: ufw", "Conflicts: ufw", "Replaces: iptables-persistent (<< 1.0.20)"} {
		if !strings.Contains(string(amdPackages), want) {
			t.Errorf("amd64 Packages missing %q", want)
		}
	}

	// Packages.gz exists and decompresses to the same bytes as Packages.
	plain, err := os.ReadFile(filepath.Join(stage, "apt-repo", "dists", codename,
		"main", "binary-amd64", "Packages"))
	if err != nil {
		t.Fatal(err)
	}
	gzPath := filepath.Join(stage, "apt-repo", "dists", codename, "main", "binary-amd64", "Packages.gz")
	gzFile, err := os.Open(gzPath)
	if err != nil {
		t.Fatalf("open gz: %v", err)
	}
	defer gzFile.Close()
	gzr, err := gzip.NewReader(gzFile)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	decompressed, err := io.ReadAll(gzr)
	if err != nil {
		t.Fatalf("decompress: %v", err)
	}
	if string(decompressed) != string(plain) {
		t.Error("Packages.gz content does not match Packages")
	}

	// Release lists every component-arch with three hash blocks.
	releasePath := filepath.Join(stage, "apt-repo", "dists", codename, "Release")
	releaseBytes, err := os.ReadFile(releasePath)
	if err != nil {
		t.Fatalf("missing Release: %v", err)
	}
	release := string(releaseBytes)
	for _, want := range []string{
		"Origin: aether-ops",
		"Suite: " + codename,
		"Codename: " + codename,
		"Components: main",
		"Architectures: all amd64",
		"MD5Sum:",
		"SHA1:",
		"SHA256:",
		"main/binary-amd64/Packages",
		"main/binary-amd64/Packages.gz",
		"main/binary-all/Packages",
		"main/binary-all/Packages.gz",
	} {
		if !strings.Contains(release, want) {
			t.Errorf("Release missing %q\n--- content ---\n%s", want, release)
		}
	}
}

func TestBuildAptRepo_NoOpOnEmpty(t *testing.T) {
	stage := t.TempDir()
	if err := BuildAptRepo(stage, nil, []string{"noble"}); err != nil {
		t.Fatalf("BuildAptRepo on empty pkgs should be a no-op, got %v", err)
	}
	if err := BuildAptRepo(stage, []*deb.Package{fixturePackage("x", "1", "amd64", "pool/main/x/x_1_amd64.deb")}, nil); err != nil {
		t.Fatalf("BuildAptRepo with no codenames should be a no-op, got %v", err)
	}
	// Nothing should have been written.
	if _, err := os.Stat(filepath.Join(stage, "apt-repo")); !os.IsNotExist(err) {
		t.Errorf("expected apt-repo/ to not exist after no-op BuildAptRepo, got err=%v", err)
	}
}

func TestBuildAptRepo_FailsWithoutRawStanza(t *testing.T) {
	stage := t.TempDir()
	pkgs := []*deb.Package{
		{Name: "broken", Version: "1.0", Arch: "amd64"}, // no RawStanza
	}
	err := BuildAptRepo(stage, pkgs, []string{"noble"})
	if err == nil || !strings.Contains(err.Error(), "RawStanza") {
		t.Fatalf("expected RawStanza error, got %v", err)
	}
}

func TestBuildAptRepo_RejectsMultipleCodenames(t *testing.T) {
	stage := t.TempDir()
	pkgs := []*deb.Package{fixturePackage("x", "1", "amd64", "pool/main/x/x_1_amd64.deb")}
	err := BuildAptRepo(stage, pkgs, []string{"noble", "jammy"})
	if err == nil || !strings.Contains(err.Error(), "multi-codename") {
		t.Fatalf("expected multi-codename rejection, got %v", err)
	}
}
