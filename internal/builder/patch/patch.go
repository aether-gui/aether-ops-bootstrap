// Package patch provides reusable, source-agnostic mutations that
// bundle builders apply to staged content before hashing it into the
// bundle manifest. Provider packages (onramp, helm charts, debs, …)
// pick which Actions to run; this package owns only the primitives.
package patch

import "fmt"

// Action is a named, idempotent mutation applied to a staging tree at
// rootDir. Actions must be safe to re-run: the second Apply is a no-op.
//
// Apply must return an error when the expected starting state is
// missing (file absent, YAML key path not found, …). Silent skips
// would let upstream drift ship a subtly wrong bundle.
type Action interface {
	Name() string
	Apply(rootDir string) error
}

// ApplyAll runs actions in order, stopping on the first error and
// wrapping it with the failing action's name.
func ApplyAll(rootDir string, actions []Action) error {
	for _, a := range actions {
		if err := a.Apply(rootDir); err != nil {
			return fmt.Errorf("patch %q: %w", a.Name(), err)
		}
	}
	return nil
}
