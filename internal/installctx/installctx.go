// Package installctx carries values the launcher resolves once at the
// start of an install and that individual components read during their
// Plan and Apply phases. Living in its own tiny package avoids the
// cyclic import that would result from components reaching back into
// the launcher package for these helpers (launcher already imports
// every component package to build the registry).
package installctx

import (
	"context"
	"fmt"
	"strings"
)

type onrampPasswordKey struct{}

// WithOnrampPassword returns a context that carries the resolved onramp
// user password. The launcher sets this once; components that need to
// create the user, configure authentication, or inject the password into
// onramp's inventory read it back via OnrampPasswordFromContext.
func WithOnrampPassword(ctx context.Context, password string) context.Context {
	return context.WithValue(ctx, onrampPasswordKey{}, password)
}

// OnrampPasswordFromContext returns the onramp password recorded on the
// context, or the empty string if none is set.
func OnrampPasswordFromContext(ctx context.Context) string {
	v, _ := ctx.Value(onrampPasswordKey{}).(string)
	return v
}

// ValidateOnrampPassword rejects any password that would be unsafe to
// feed into chpasswd stdin or write into an ansible hosts.ini line.
//
// The forbidden characters are:
//
//   - '\x00' (NUL) — truncates C-string APIs and most file-I/O paths,
//     so its presence is almost certainly an accidental paste.
//   - '\r' and '\n' — chpasswd reads "user:pass" records one per line,
//     and hosts.ini is line-oriented. A newline here would inject an
//     additional chpasswd record (potentially altering another user's
//     password) or break out of the inventory node line.
//
// Empty passwords are also rejected. Every other printable character
// (including spaces, tabs, '#', quotes) is allowed; downstream writers
// quote appropriately for their target format.
//
// The launcher validates once in ResolveOnrampPassword. Individual write
// sites call this again as defense-in-depth — a test harness that stuffs
// a value onto the context directly should still fail closed.
func ValidateOnrampPassword(password string) error {
	if password == "" {
		return fmt.Errorf("password is empty")
	}
	if i := strings.IndexAny(password, "\x00\r\n"); i >= 0 {
		return fmt.Errorf("password contains an unsupported control character (offset %d); NUL, CR, and LF are rejected", i)
	}
	return nil
}
