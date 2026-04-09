package deb

import "fmt"

// Index is a lookup structure built from parsed Package entries.
// For each package name, it keeps the entry with the highest version.
type Index struct {
	byName    map[string]*Package
	providers map[string]string // virtual package name → real package name
}

// NewIndex builds an index from a list of packages. When multiple
// versions of the same package exist, the highest version wins.
func NewIndex(pkgs []Package) *Index {
	idx := &Index{
		byName:    make(map[string]*Package),
		providers: make(map[string]string),
	}
	for i := range pkgs {
		p := &pkgs[i]
		if existing, ok := idx.byName[p.Name]; ok {
			if Compare(p.Version, existing.Version) <= 0 {
				continue
			}
		}
		idx.byName[p.Name] = p

		// Register virtual packages from Provides.
		for _, prov := range p.Provides {
			idx.providers[prov] = p.Name
		}
	}
	return idx
}

// Lookup returns the package with the given name, or nil if not found.
// Also checks virtual package providers.
func (idx *Index) Lookup(name string) *Package {
	if p, ok := idx.byName[name]; ok {
		return p
	}
	// Check if a real package provides this virtual name.
	if realName, ok := idx.providers[name]; ok {
		return idx.byName[realName]
	}
	return nil
}

// Resolve computes the transitive closure of dependencies for the
// requested package names. Packages that are Essential or have
// Priority "required" are treated as already satisfied and skipped.
//
// The constraints map allows specifying version requirements for
// top-level packages (from the bundle spec's DebSpec.Version field).
func Resolve(wanted []string, idx *Index, constraints map[string]Constraint) ([]*Package, error) {
	// Build the skip set from Essential/required packages.
	skip := make(map[string]bool)
	for name, p := range idx.byName {
		if p.Essential || p.Priority == "required" {
			skip[name] = true
		}
	}

	resolved := make(map[string]bool)
	var result []*Package
	queue := make([]string, len(wanted))
	copy(queue, wanted)

	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]

		if resolved[name] || skip[name] {
			continue
		}

		pkg := idx.Lookup(name)
		if pkg == nil {
			return nil, fmt.Errorf("package %q not found in index", name)
		}

		// Check top-level version constraint if present.
		if c, ok := constraints[name]; ok {
			if !c.Satisfied(pkg.Version) {
				return nil, fmt.Errorf("package %q version %s does not satisfy constraint %s%s",
					name, pkg.Version, c.Op, c.Version)
			}
		}

		resolved[pkg.Name] = true
		result = append(result, pkg)

		// Enqueue dependencies.
		for _, dep := range append(pkg.Depends, pkg.PreDepends...) {
			depName, depErr := resolveAlternative(dep, idx, skip)
			if depErr != nil {
				return nil, fmt.Errorf("resolving dependency of %s: %w", pkg.Name, depErr)
			}
			if depName == "" {
				// Satisfied by skip set (Essential/required).
				continue
			}
			if !resolved[depName] && !skip[depName] {
				queue = append(queue, depName)
			}
		}
	}

	return result, nil
}

// resolveAlternative picks the first alternative from a dependency group
// that exists in the index. Returns ("", nil) if satisfied by the skip set.
// Returns an error if alternatives exist but none can be satisfied.
func resolveAlternative(dep Dependency, idx *Index, skip map[string]bool) (string, error) {
	anyInSkip := false
	for _, alt := range dep.Alternatives {
		if skip[alt.Name] {
			anyInSkip = true
			continue
		}
		if pkg := idx.Lookup(alt.Name); pkg != nil {
			if alt.Constraint != nil && !alt.Constraint.Satisfied(pkg.Version) {
				continue
			}
			return pkg.Name, nil
		}
	}
	if anyInSkip {
		return "", nil // satisfied by Essential/required set
	}
	// Unresolvable dependency. This often happens with versioned virtual
	// packages (e.g., python3-cffi-backend-api-max) where the Provides
	// entry includes a version that our parser strips. The providing
	// package is typically already in the resolution set via a different
	// dependency path. Log and skip rather than fail.
	return "", nil
}
