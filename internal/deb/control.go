package deb

import (
	"bufio"
	"io"
	"strconv"
	"strings"
)

// ParseControl parses a Debian Packages index (RFC 822 format) from r
// and returns the package entries it contains. Packages are separated
// by blank lines.
func ParseControl(r io.Reader) ([]Package, error) {
	var packages []Package
	fields := make(map[string]string)

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024) // 1MB buffer for large Description fields
	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			// Blank line: emit package if we have fields.
			if len(fields) > 0 {
				packages = append(packages, packageFromFields(fields))
				fields = make(map[string]string)
			}
			continue
		}

		// Continuation line (starts with space or tab).
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			// Append to the last field seen — but we don't track which
			// field was last, and for our purposes we don't need multi-line
			// values (Description is the main one and we skip it).
			continue
		}

		// Key: Value line.
		if i := strings.Index(line, ":"); i >= 0 {
			key := line[:i]
			value := strings.TrimSpace(line[i+1:])
			fields[key] = value
		}
	}

	// Emit the last package if the file doesn't end with a blank line.
	if len(fields) > 0 {
		packages = append(packages, packageFromFields(fields))
	}

	return packages, scanner.Err()
}

func packageFromFields(fields map[string]string) Package {
	p := Package{
		Name:     fields["Package"],
		Version:  fields["Version"],
		Arch:     fields["Architecture"],
		Filename: fields["Filename"],
		SHA256:   fields["SHA256"],
		Priority: fields["Priority"],
	}

	if s := fields["Size"]; s != "" {
		p.Size, _ = strconv.ParseInt(s, 10, 64)
	}

	if strings.EqualFold(fields["Essential"], "yes") {
		p.Essential = true
	}

	if s := fields["Depends"]; s != "" {
		p.Depends = ParseDependsField(s)
	}
	if s := fields["Pre-Depends"]; s != "" {
		p.PreDepends = ParseDependsField(s)
	}
	if s := fields["Provides"]; s != "" {
		for _, prov := range strings.Split(s, ",") {
			name := strings.TrimSpace(prov)
			// Strip version from provides (e.g., "git-core (= 1:2.43)")
			if i := strings.Index(name, "("); i >= 0 {
				name = strings.TrimSpace(name[:i])
			}
			if name != "" {
				p.Provides = append(p.Provides, name)
			}
		}
	}

	return p
}

// ParseDependsField parses a Depends or Pre-Depends field value into
// structured dependency groups. The field uses commas for AND and
// pipes for OR (alternatives).
// Example: "libc6 (>= 2.38), default-mta | mail-transport-agent"
func ParseDependsField(s string) []Dependency {
	var deps []Dependency
	for _, group := range strings.Split(s, ",") {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		var alts []DepAlternative
		for _, alt := range strings.Split(group, "|") {
			alt = strings.TrimSpace(alt)
			if alt == "" {
				continue
			}
			alts = append(alts, parseOneDepAlternative(alt))
		}
		if len(alts) > 0 {
			deps = append(deps, Dependency{Alternatives: alts})
		}
	}
	return deps
}

// parseOneDepAlternative parses "libc6 (>= 2.38)" or just "libc6".
func parseOneDepAlternative(s string) DepAlternative {
	// Strip architecture qualifier like ":amd64" or ":any".
	s = strings.TrimSpace(s)

	var name, constraintStr string
	if i := strings.Index(s, "("); i >= 0 {
		name = strings.TrimSpace(s[:i])
		if j := strings.Index(s, ")"); j > i {
			constraintStr = strings.TrimSpace(s[i+1 : j])
		}
	} else {
		name = s
	}

	// Strip :arch qualifier.
	if i := strings.Index(name, ":"); i >= 0 {
		name = name[:i]
	}

	// Strip any [arch] restrictions.
	if i := strings.Index(name, "["); i >= 0 {
		name = strings.TrimSpace(name[:i])
	}

	alt := DepAlternative{Name: name}
	if constraintStr != "" {
		if c, err := ParseConstraint(constraintStr); err == nil {
			alt.Constraint = &c
		}
	}

	return alt
}
