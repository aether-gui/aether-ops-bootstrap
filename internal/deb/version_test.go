package deb

import "testing"

func TestCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		// Equal.
		{"1.0", "1.0", 0},
		{"1:1.0", "1:1.0", 0},
		{"1.0-1", "1.0-1", 0},

		// Epoch dominates.
		{"2:1.0", "1:9.0", 1},
		{"1:1.0", "2:0.1", -1},
		{"1:1.0", "1.0", 1}, // epoch 1 > implicit epoch 0

		// Upstream comparison.
		{"1.1", "1.0", 1},
		{"1.0", "1.1", -1},
		{"1.10", "1.9", 1}, // numeric comparison, not lexicographic
		{"1.2.3", "1.2.3", 0},
		{"1.2.4", "1.2.3", 1},

		// Tilde sorts before everything.
		{"1.0~beta1", "1.0", -1},
		{"1.0~beta2", "1.0~beta1", 1},
		{"1.0~", "1.0", -1},

		// Revision comparison.
		{"1.0-2", "1.0-1", 1},
		{"1.0-1", "1.0-2", -1},
		{"1.0", "1.0-1", -1}, // no revision < any revision

		// Real Ubuntu version strings.
		{"1:2.43.0-1ubuntu7", "1:2.43.0-1ubuntu6", 1},
		{"1:2.43.0-1ubuntu7", "1:2.43.0-1ubuntu7", 0},
		{"9.2.0+dfsg-0ubuntu5", "9.1.0+dfsg-0ubuntu1", 1},

		// Letters vs digits ordering.
		{"1.0a", "1.0", 1},
	}

	for _, tt := range tests {
		got := Compare(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestParseVersion(t *testing.T) {
	tests := []struct {
		input    string
		epoch    int
		upstream string
		revision string
	}{
		{"1.0", 0, "1.0", ""},
		{"1:2.0", 1, "2.0", ""},
		{"1.0-1", 0, "1.0", "1"},
		{"2:1.0-3ubuntu1", 2, "1.0", "3ubuntu1"},
		{"1:2.43.0-1ubuntu7", 1, "2.43.0", "1ubuntu7"},
	}

	for _, tt := range tests {
		v := ParseVersion(tt.input)
		if v.Epoch != tt.epoch {
			t.Errorf("ParseVersion(%q).Epoch = %d, want %d", tt.input, v.Epoch, tt.epoch)
		}
		if v.Upstream != tt.upstream {
			t.Errorf("ParseVersion(%q).Upstream = %q, want %q", tt.input, v.Upstream, tt.upstream)
		}
		if v.Revision != tt.revision {
			t.Errorf("ParseVersion(%q).Revision = %q, want %q", tt.input, v.Revision, tt.revision)
		}
	}
}

func TestConstraintSatisfied(t *testing.T) {
	tests := []struct {
		constraint string
		version    string
		want       bool
	}{
		{">=2.14", "9.2.0", true},
		{">=2.14", "2.14", true},
		{">=2.14", "2.13", false},
		{">>1.0", "1.1", true},
		{">>1.0", "1.0", false},
		{"<<2.0", "1.9", true},
		{"<<2.0", "2.0", false},
		{"<=2.0", "2.0", true},
		{"<=2.0", "2.1", false},
		{"=1.0", "1.0", true},
		{"=1.0", "1.1", false},
	}

	for _, tt := range tests {
		c, err := ParseConstraint(tt.constraint)
		if err != nil {
			t.Fatalf("ParseConstraint(%q): %v", tt.constraint, err)
		}
		got := c.Satisfied(tt.version)
		if got != tt.want {
			t.Errorf("Constraint(%q).Satisfied(%q) = %v, want %v", tt.constraint, tt.version, got, tt.want)
		}
	}
}

func TestParseConstraintErrors(t *testing.T) {
	for _, s := range []string{"", "foobar", ">=", "<< "} {
		if _, err := ParseConstraint(s); err == nil {
			t.Errorf("ParseConstraint(%q) should fail", s)
		}
	}
}
