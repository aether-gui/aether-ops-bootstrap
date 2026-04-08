package deb

import (
	"errors"
	"io"
)

// ErrNotImplemented is returned by stub implementations.
var ErrNotImplemented = errors.New("not implemented")

// Package describes a single Debian package parsed from a Packages index.
type Package struct {
	Name     string
	Version  string
	Arch     string
	Depends  []string
	Filename string
	SHA256   string
}

// ParseControl parses a Debian Packages control file from r and returns
// the package entries it contains. The control file format uses
// RFC 822-style paragraphs separated by blank lines.
func ParseControl(r io.Reader) ([]Package, error) {
	return nil, ErrNotImplemented
}

// ResolveDeps computes the transitive closure of dependencies for the
// requested package names, drawn from the available package set.
func ResolveDeps(wanted []string, available []Package) ([]Package, error) {
	return nil, ErrNotImplemented
}
