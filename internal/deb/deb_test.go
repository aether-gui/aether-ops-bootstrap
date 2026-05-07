package deb

import (
	"strings"
	"testing"
)

func TestParseControl(t *testing.T) {
	input := `Package: git
Version: 1:2.43.0-1ubuntu7
Architecture: amd64
Depends: libc6 (>= 2.38), libcurl3t64-gnutls (>= 7.56.1), perl, git-man (>> 1:2.43.0)
Pre-Depends: dpkg (>= 1.15.6~)
Provides: git-completion, git-core
Filename: pool/main/g/git/git_2.43.0-1ubuntu7_amd64.deb
Size: 3673594
SHA256: abc123def456
Priority: optional
Essential: no

Package: dpkg
Version: 1.22.6ubuntu6
Architecture: amd64
Depends: tar (>= 1.28-1)
Filename: pool/main/d/dpkg/dpkg_1.22.6ubuntu6_amd64.deb
Size: 1370000
SHA256: 789abc
Priority: required
Essential: yes

Package: ansible
Version: 9.2.0+dfsg-0ubuntu5
Architecture: all
Depends: python3, python3-jinja2
Filename: pool/universe/a/ansible/ansible_9.2.0+dfsg-0ubuntu5_all.deb
Size: 25000000
SHA256: ansible123
Priority: optional

`

	pkgs, err := ParseControl(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseControl: %v", err)
	}

	if len(pkgs) != 3 {
		t.Fatalf("len(pkgs) = %d, want 3", len(pkgs))
	}

	// git
	git := pkgs[0]
	if git.Name != "git" {
		t.Errorf("pkgs[0].Name = %q", git.Name)
	}
	if git.Version != "1:2.43.0-1ubuntu7" {
		t.Errorf("git.Version = %q", git.Version)
	}
	if git.Arch != "amd64" {
		t.Errorf("git.Arch = %q", git.Arch)
	}
	if len(git.Depends) != 4 {
		t.Fatalf("len(git.Depends) = %d, want 4", len(git.Depends))
	}
	if git.Depends[0].Alternatives[0].Name != "libc6" {
		t.Errorf("git.Depends[0] name = %q", git.Depends[0].Alternatives[0].Name)
	}
	if git.Depends[0].Alternatives[0].Constraint == nil || git.Depends[0].Alternatives[0].Constraint.Op != ">=" {
		t.Error("git.Depends[0] should have >= constraint")
	}
	if len(git.PreDepends) != 1 {
		t.Fatalf("len(git.PreDepends) = %d, want 1", len(git.PreDepends))
	}
	if len(git.Provides) != 2 {
		t.Fatalf("len(git.Provides) = %d, want 2", len(git.Provides))
	}
	if git.SHA256 != "abc123def456" {
		t.Errorf("git.SHA256 = %q", git.SHA256)
	}
	if git.Size != 3673594 {
		t.Errorf("git.Size = %d", git.Size)
	}

	// dpkg
	dpkg := pkgs[1]
	if !dpkg.Essential {
		t.Error("dpkg should be Essential")
	}
	if dpkg.Priority != "required" {
		t.Errorf("dpkg.Priority = %q", dpkg.Priority)
	}

	// ansible
	ansible := pkgs[2]
	if ansible.Arch != "all" {
		t.Errorf("ansible.Arch = %q", ansible.Arch)
	}
}

func TestParseDependsField(t *testing.T) {
	tests := []struct {
		input    string
		wantLen  int
		firstAlt int // number of alternatives in first group
	}{
		{"libc6 (>= 2.38)", 1, 1},
		{"libc6 (>= 2.38), perl", 2, 1},
		{"default-mta | mail-transport-agent", 1, 2},
		{"libc6 (>= 2.38), default-mta | mail-transport-agent, perl", 3, 1},
		{"", 0, 0},
	}

	for _, tt := range tests {
		deps := ParseDependsField(tt.input)
		if len(deps) != tt.wantLen {
			t.Errorf("ParseDependsField(%q) len = %d, want %d", tt.input, len(deps), tt.wantLen)
			continue
		}
		if tt.wantLen > 0 && len(deps[0].Alternatives) != tt.firstAlt {
			t.Errorf("ParseDependsField(%q) first alts = %d, want %d", tt.input, len(deps[0].Alternatives), tt.firstAlt)
		}
	}
}

func TestParseOneDepWithArchQualifier(t *testing.T) {
	deps := ParseDependsField("libc6:amd64 (>= 2.38)")
	if len(deps) != 1 {
		t.Fatalf("len = %d", len(deps))
	}
	if deps[0].Alternatives[0].Name != "libc6" {
		t.Errorf("name = %q, want libc6 (arch qualifier stripped)", deps[0].Alternatives[0].Name)
	}
}

// TestParseControlPreservesRawStanza guards the contract used by the
// local apt-repo builder: every parsed package keeps its original RFC 822
// stanza bytes so downstream consumers can re-emit Packages content
// without losing fields the parser does not extract — notably Breaks,
// Conflicts, Replaces, and folded Description lines.
func TestParseControlPreservesRawStanza(t *testing.T) {
	input := `Package: iptables-persistent
Version: 1.0.20
Architecture: all
Depends: iptables, netfilter-persistent (>= 1.0.20)
Breaks: ufw
Conflicts: ufw
Replaces: iptables-persistent (<< 1.0.20)
Filename: pool/main/i/iptables-persistent/iptables-persistent_1.0.20_all.deb
Size: 12345
SHA256: abc123
Priority: optional
Description: boot-time loader for netfilter rules
 This package contains scripts to load iptables-save format rules at
 boot. Two-line description body to exercise continuation handling.

Package: ufw
Version: 0.36.2-6
Architecture: all
Filename: pool/main/u/ufw/ufw_0.36.2-6_all.deb
Size: 67890
SHA256: def456
Priority: optional

`

	pkgs, err := ParseControl(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseControl: %v", err)
	}
	if len(pkgs) != 2 {
		t.Fatalf("len(pkgs) = %d, want 2", len(pkgs))
	}

	// First stanza: every line of the original (including folded
	// description and Breaks/Conflicts/Replaces) must round-trip.
	first := string(pkgs[0].RawStanza)
	for _, want := range []string{
		"Package: iptables-persistent\n",
		"Breaks: ufw\n",
		"Conflicts: ufw\n",
		"Replaces: iptables-persistent (<< 1.0.20)\n",
		"Description: boot-time loader for netfilter rules\n",
		" This package contains scripts to load iptables-save format rules at\n",
		" boot. Two-line description body to exercise continuation handling.\n",
	} {
		if !strings.Contains(first, want) {
			t.Errorf("RawStanza missing %q\n--- got ---\n%s", want, first)
		}
	}

	// Stanza separator (blank line) must NOT be part of the captured
	// bytes — that's the framing, not the stanza.
	if strings.HasPrefix(first, "\n") || strings.HasSuffix(first, "\n\n") {
		t.Errorf("RawStanza should not include blank-line framing; got %q", first)
	}

	// Second stanza is captured independently.
	second := string(pkgs[1].RawStanza)
	if !strings.Contains(second, "Package: ufw\n") {
		t.Errorf("second RawStanza missing Package line: %q", second)
	}
	if strings.Contains(second, "iptables-persistent") {
		t.Errorf("second stanza leaked content from first: %q", second)
	}
}
