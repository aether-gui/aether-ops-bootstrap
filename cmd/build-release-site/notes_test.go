package main

import (
	"strings"
	"testing"
)

func TestParseNotesFile(t *testing.T) {
	tests := []struct {
		name        string
		in          string
		wantSummary string
		wantBullets []string
	}{
		{
			name:        "empty",
			in:          "",
			wantSummary: "",
			wantBullets: nil,
		},
		{
			name:        "whitespace only",
			in:          "   \n\n  \t  \n",
			wantSummary: "",
			wantBullets: nil,
		},
		{
			name: "bullets only",
			in: `- First bullet.
- Second bullet wraps
  across two lines.
- Third bullet.`,
			wantSummary: "",
			wantBullets: []string{
				"First bullet.",
				"Second bullet wraps across two lines.",
				"Third bullet.",
			},
		},
		{
			name: "summary then bullets",
			in: `Summary headline goes here on one line.

- First bullet.
- Second bullet.`,
			wantSummary: "Summary headline goes here on one line.",
			wantBullets: []string{
				"First bullet.",
				"Second bullet.",
			},
		},
		{
			name: "summary wraps before bullets",
			in: `Summary headline spans
two physical lines.

- Only bullet.`,
			wantSummary: "Summary headline spans two physical lines.",
			wantBullets: []string{"Only bullet."},
		},
		{
			name: "summary then bullets without blank line",
			in: `Summary headline.
- First bullet.
- Second bullet.`,
			wantSummary: "Summary headline.",
			wantBullets: []string{"First bullet.", "Second bullet."},
		},
		{
			name: "asterisk bullets accepted",
			in: `Headline.

* Star bullet one.
* Star bullet two.`,
			wantSummary: "Headline.",
			wantBullets: []string{"Star bullet one.", "Star bullet two."},
		},
		{
			name: "paragraph mode (no bullet prefixes)",
			in: `First paragraph is the summary.
It wraps to two lines.

Second paragraph becomes the first bullet.

Third paragraph becomes the second bullet.`,
			wantSummary: "First paragraph is the summary. It wraps to two lines.",
			wantBullets: []string{
				"Second paragraph becomes the first bullet.",
				"Third paragraph becomes the second bullet.",
			},
		},
		{
			name:        "paragraph mode single paragraph",
			in:          `Just a summary with no bullets.`,
			wantSummary: "Just a summary with no bullets.",
			wantBullets: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotSummary, gotBullets := parseNotesFile(tc.in)
			if gotSummary != tc.wantSummary {
				t.Errorf("summary = %q, want %q", gotSummary, tc.wantSummary)
			}
			if !equalStringSlice(gotBullets, tc.wantBullets) {
				t.Errorf("bullets = %v, want %v", gotBullets, tc.wantBullets)
			}
		})
	}
}

func TestDefaultNotes(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"nil", nil, []string{"No release notes provided."}},
		{"empty slice", []string{}, []string{"No release notes provided."}},
		{"only whitespace entries", []string{"", "   "}, []string{"No release notes provided."}},
		{"trims entries", []string{"  bullet one  ", "bullet two"}, []string{"bullet one", "bullet two"}},
		{"drops empty entries", []string{"first", "", "  ", "second"}, []string{"first", "second"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := defaultNotes(tc.in)
			if !equalStringSlice(got, tc.want) {
				t.Errorf("defaultNotes(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if strings.TrimSpace(a[i]) != strings.TrimSpace(b[i]) {
			return false
		}
	}
	return true
}
