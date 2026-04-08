package deb

import (
	"errors"
	"strings"
	"testing"
)

func TestParseControlReturnsNotImplemented(t *testing.T) {
	_, err := ParseControl(strings.NewReader(""))
	if !errors.Is(err, ErrNotImplemented) {
		t.Errorf("ParseControl error = %v, want ErrNotImplemented", err)
	}
}

func TestResolveDepsReturnsNotImplemented(t *testing.T) {
	_, err := ResolveDeps([]string{"git"}, nil)
	if !errors.Is(err, ErrNotImplemented) {
		t.Errorf("ResolveDeps error = %v, want ErrNotImplemented", err)
	}
}

func TestPackageStruct(t *testing.T) {
	p := Package{
		Name:     "git",
		Version:  "1:2.43.0-1ubuntu7",
		Arch:     "amd64",
		Depends:  []string{"libc6", "libcurl4"},
		Filename: "pool/main/g/git/git_2.43.0-1ubuntu7_amd64.deb",
		SHA256:   "abc123",
	}

	if p.Name != "git" {
		t.Errorf("Name = %q, want %q", p.Name, "git")
	}
	if len(p.Depends) != 2 {
		t.Errorf("len(Depends) = %d, want 2", len(p.Depends))
	}
}
