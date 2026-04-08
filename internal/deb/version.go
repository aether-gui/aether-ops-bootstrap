package deb

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// Version represents a parsed Debian version string: [epoch:]upstream[-revision]
type Version struct {
	Epoch    int
	Upstream string
	Revision string
}

// ParseVersion parses a Debian version string into its components.
func ParseVersion(s string) Version {
	var v Version

	// Extract epoch.
	if i := strings.Index(s, ":"); i >= 0 {
		v.Epoch, _ = strconv.Atoi(s[:i])
		s = s[i+1:]
	}

	// Extract revision (last hyphen).
	if i := strings.LastIndex(s, "-"); i >= 0 {
		v.Upstream = s[:i]
		v.Revision = s[i+1:]
	} else {
		v.Upstream = s
	}

	return v
}

// Compare compares two Debian version strings.
// Returns -1 if a < b, 0 if a == b, +1 if a > b.
func Compare(a, b string) int {
	va := ParseVersion(a)
	vb := ParseVersion(b)

	if va.Epoch != vb.Epoch {
		if va.Epoch < vb.Epoch {
			return -1
		}
		return 1
	}

	if c := compareFragment(va.Upstream, vb.Upstream); c != 0 {
		return c
	}

	return compareFragment(va.Revision, vb.Revision)
}

// compareFragment compares two version fragments using the Debian
// algorithm: alternate between non-digit and digit segments, comparing
// non-digit segments lexicographically (with special ~ ordering) and
// digit segments numerically.
func compareFragment(a, b string) int {
	ai, bi := 0, 0

	for ai < len(a) || bi < len(b) {
		// Compare non-digit prefix.
		var aNonDigit, bNonDigit string
		for ai < len(a) && !isDigit(a[ai]) {
			aNonDigit += string(a[ai])
			ai++
		}
		for bi < len(b) && !isDigit(b[bi]) {
			bNonDigit += string(b[bi])
			bi++
		}
		if c := compareLex(aNonDigit, bNonDigit); c != 0 {
			return c
		}

		// Compare digit segment numerically.
		var aDigit, bDigit string
		for ai < len(a) && isDigit(a[ai]) {
			aDigit += string(a[ai])
			ai++
		}
		for bi < len(b) && isDigit(b[bi]) {
			bDigit += string(b[bi])
			bi++
		}
		// An empty digit segment sorts before a non-empty one.
		if aDigit == "" && bDigit != "" {
			return -1
		}
		if aDigit != "" && bDigit == "" {
			return 1
		}
		aNum := trimLeadingZeros(aDigit)
		bNum := trimLeadingZeros(bDigit)
		if len(aNum) != len(bNum) {
			if len(aNum) < len(bNum) {
				return -1
			}
			return 1
		}
		if aNum < bNum {
			return -1
		}
		if aNum > bNum {
			return 1
		}
	}

	return 0
}

// compareLex compares two non-digit version segments using Debian ordering:
// ~ < empty < letters < everything else
func compareLex(a, b string) int {
	ai, bi := 0, 0
	for ai < len(a) || bi < len(b) {
		var ac, bc int
		if ai < len(a) {
			ac = charOrder(a[ai])
			ai++
		}
		if bi < len(b) {
			bc = charOrder(b[bi])
			bi++
		}
		if ac != bc {
			if ac < bc {
				return -1
			}
			return 1
		}
	}
	return 0
}

// charOrder returns the sort order of a byte in the Debian version
// comparison algorithm: ~ sorts before everything (including empty),
// letters sort before non-letters, everything else sorts after.
func charOrder(c byte) int {
	switch {
	case c == '~':
		return -1
	case c == 0:
		return 0
	case unicode.IsLetter(rune(c)):
		return int(c)
	default:
		return int(c) + 256
	}
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

func trimLeadingZeros(s string) string {
	s = strings.TrimLeft(s, "0")
	if s == "" {
		return "0"
	}
	return s
}

// Constraint represents a version constraint like ">= 2.14" or "= 1.2.3-1".
type Constraint struct {
	Op      string // ">=", ">>", "<=", "<<", "="
	Version string
}

// ParseConstraint parses a constraint string like ">=2.14" or ">= 2.14".
func ParseConstraint(s string) (Constraint, error) {
	s = strings.TrimSpace(s)
	for _, op := range []string{">=", ">>", "<=", "<<", "="} {
		if strings.HasPrefix(s, op) {
			ver := strings.TrimSpace(s[len(op):])
			if ver == "" {
				return Constraint{}, fmt.Errorf("empty version in constraint %q", s)
			}
			return Constraint{Op: op, Version: ver}, nil
		}
	}
	return Constraint{}, fmt.Errorf("invalid constraint %q", s)
}

// Satisfied returns true if the given version satisfies this constraint.
func (c Constraint) Satisfied(version string) bool {
	cmp := Compare(version, c.Version)
	switch c.Op {
	case ">=":
		return cmp >= 0
	case ">>":
		return cmp > 0
	case "<=":
		return cmp <= 0
	case "<<":
		return cmp < 0
	case "=":
		return cmp == 0
	default:
		return false
	}
}
