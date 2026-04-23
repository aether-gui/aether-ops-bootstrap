package launcher

import (
	"strings"
	"testing"
)

func TestResolveOnrampPassword_Precedence(t *testing.T) {
	tests := []struct {
		name          string
		cli           string
		env           string
		manifest      string
		wantPassword  string
		wantSource    string
		wantGenerated bool
	}{
		{
			name:         "flag beats env and spec",
			cli:          "from-flag",
			env:          "from-env",
			manifest:     "from-spec",
			wantPassword: "from-flag",
			wantSource:   "flag",
		},
		{
			name:         "env beats spec when no flag",
			env:          "from-env",
			manifest:     "from-spec",
			wantPassword: "from-env",
			wantSource:   "env",
		},
		{
			name:         "spec used when no flag or env",
			manifest:     "from-spec",
			wantPassword: "from-spec",
			wantSource:   "spec",
		},
		{
			name:          "random generated when no source supplied",
			wantSource:    "generated",
			wantGenerated: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.env != "" {
				t.Setenv(OnrampPasswordEnvVar, tt.env)
			} else {
				t.Setenv(OnrampPasswordEnvVar, "")
			}
			got, src, err := ResolveOnrampPassword(tt.cli, tt.manifest)
			if err != nil {
				t.Fatalf("ResolveOnrampPassword: %v", err)
			}
			if src != tt.wantSource {
				t.Errorf("source = %q, want %q", src, tt.wantSource)
			}
			if tt.wantGenerated {
				if got == "" {
					t.Error("generated password should be non-empty")
				}
				if len(got) < 16 {
					t.Errorf("generated password too short: len=%d (%q)", len(got), got)
				}
				return
			}
			if got != tt.wantPassword {
				t.Errorf("password = %q, want %q", got, tt.wantPassword)
			}
		})
	}
}

func TestResolveOnrampPassword_GeneratedIsRandom(t *testing.T) {
	// A fresh invocation with no sources should produce a different
	// value each time. If this ever returns a constant, crypto/rand
	// is either stubbed or broken and we want to hear about it loudly.
	t.Setenv(OnrampPasswordEnvVar, "")

	first, _, err := ResolveOnrampPassword("", "")
	if err != nil {
		t.Fatalf("ResolveOnrampPassword: %v", err)
	}
	second, _, err := ResolveOnrampPassword("", "")
	if err != nil {
		t.Fatalf("ResolveOnrampPassword: %v", err)
	}
	if first == second {
		t.Fatalf("two generated passwords match (%q); random source broken", first)
	}
}

func TestResolveOnrampPassword_EnvEmptyFallsThrough(t *testing.T) {
	// An env var explicitly set to the empty string should behave the
	// same as unset — the resolver should fall through to the next
	// source, not treat "" as a valid password.
	t.Setenv(OnrampPasswordEnvVar, "")
	got, src, err := ResolveOnrampPassword("", "from-spec")
	if err != nil {
		t.Fatalf("ResolveOnrampPassword: %v", err)
	}
	if src != "spec" {
		t.Errorf("source = %q, want %q", src, "spec")
	}
	if got != "from-spec" {
		t.Errorf("password = %q, want %q", got, "from-spec")
	}
}

func TestGenerateRandomPassword_Length(t *testing.T) {
	for _, n := range []int{8, 16, 24, 48} {
		got, err := generateRandomPassword(n)
		if err != nil {
			t.Fatalf("generateRandomPassword(%d): %v", n, err)
		}
		if len(got) != n {
			t.Errorf("generateRandomPassword(%d) len = %d, want %d (got %q)", n, len(got), n, got)
		}
		// Base64 URL encoding uses A-Za-z0-9_- only; a generated
		// password should never contain a literal whitespace, quote,
		// or '#' that would confuse the ini-injection step.
		if strings.ContainsAny(got, " \t\n\"'#") {
			t.Errorf("generateRandomPassword(%d) contains inventory-unsafe chars: %q", n, got)
		}
	}
}
