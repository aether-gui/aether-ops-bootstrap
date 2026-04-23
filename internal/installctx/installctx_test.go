package installctx

import (
	"context"
	"strings"
	"testing"
)

func TestOnrampPasswordFromContext_DefaultEmpty(t *testing.T) {
	if got := OnrampPasswordFromContext(context.Background()); got != "" {
		t.Errorf("bare context password = %q, want empty", got)
	}
}

func TestOnrampPasswordFromContext_Roundtrip(t *testing.T) {
	ctx := WithOnrampPassword(context.Background(), "hunter2")
	if got := OnrampPasswordFromContext(ctx); got != "hunter2" {
		t.Errorf("roundtrip password = %q, want %q", got, "hunter2")
	}
}

func TestValidateOnrampPassword_AcceptsPrintable(t *testing.T) {
	// Passwords with whitespace, '#', quotes, and backslashes are all
	// acceptable; downstream writers quote them appropriately.
	ok := []string{
		"hunter2",
		"sp ace",
		"with#hash",
		"with'quote",
		`with"quote`,
		`with\backslash`,
		"mixed sp ace # ' \"",
	}
	for _, pw := range ok {
		if err := ValidateOnrampPassword(pw); err != nil {
			t.Errorf("ValidateOnrampPassword(%q) = %v, want nil", pw, err)
		}
	}
}

func TestValidateOnrampPassword_RejectsControlChars(t *testing.T) {
	tests := []struct {
		name string
		pw   string
	}{
		{"empty", ""},
		{"newline", "pass\nword"},
		{"carriage-return", "pass\rword"},
		{"nul", "pass\x00word"},
		{"trailing newline", "password\n"},
		{"leading newline", "\npassword"},
		{"chpasswd injection attempt", "pass\nroot:haxxor"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateOnrampPassword(tc.pw)
			if err == nil {
				t.Fatalf("ValidateOnrampPassword(%q) returned nil, want error", tc.pw)
			}
			if tc.pw != "" && !strings.Contains(err.Error(), "offset") {
				t.Errorf("error %q should mention the offset of the bad character", err)
			}
		})
	}
}
