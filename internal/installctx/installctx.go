// Package installctx carries values the launcher resolves once at the
// start of an install and that individual components read during their
// Plan and Apply phases. Living in its own tiny package avoids the
// cyclic import that would result from components reaching back into
// the launcher package for these helpers (launcher already imports
// every component package to build the registry).
package installctx

import "context"

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
