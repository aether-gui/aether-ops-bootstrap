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
