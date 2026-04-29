package deb

// Package describes a single Debian package parsed from a Packages index.
type Package struct {
	Name       string
	Version    string
	Arch       string
	Depends    []Dependency
	PreDepends []Dependency
	Provides   []string
	Filename   string
	SHA256     string
	Size       int64
	Essential  bool
	Priority   string // "required", "important", "standard", "optional", "extra"
	SourceName string
	SourceURL  string
}

// Dependency represents one dependency group from a Depends line.
// A group with multiple alternatives means any one of them satisfies it.
// Example: "default-mta | mail-transport-agent" has two alternatives.
type Dependency struct {
	Alternatives []DepAlternative
}

// DepAlternative is a single package reference, optionally version-constrained.
// Example: "libc6 (>= 2.38)"
type DepAlternative struct {
	Name       string
	Constraint *Constraint // nil if unconstrained
}
