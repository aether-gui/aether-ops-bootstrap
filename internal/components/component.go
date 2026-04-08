package components

import (
	"context"
	"errors"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
	"github.com/aether-gui/aether-ops-bootstrap/internal/state"
)

// ErrNotImplemented is returned by component stubs that lack real logic.
var ErrNotImplemented = errors.New("not implemented")

// Component is the interface every install/upgrade/repair module implements.
// The launcher walks components in dependency order, calling Plan to compute
// what would change and Apply to execute changes.
type Component interface {
	Name() string
	DesiredVersion(b *bundle.Manifest) string
	CurrentVersion(s *state.State) string
	Plan(current, desired string) (Plan, error)
	Apply(ctx context.Context, plan Plan) error
}

// Plan describes what a component intends to do. If NoOp is true, the
// component is already at the desired state and Apply is skipped.
type Plan struct {
	NoOp    bool
	Actions []Action
}

// Action is a single discrete step within a component's plan.
type Action struct {
	Description string
	Fn          func(ctx context.Context) error
}

// Registry holds an ordered list of components. The order determines
// the execution sequence during install/upgrade/repair.
type Registry struct {
	components []Component
}

// Register adds a component to the end of the registry.
func (r *Registry) Register(c Component) {
	r.components = append(r.components, c)
}

// All returns all registered components in order.
func (r *Registry) All() []Component {
	out := make([]Component, len(r.components))
	copy(out, r.components)
	return out
}

// ByName returns the component with the given name, or nil if not found.
func (r *Registry) ByName(name string) Component {
	for _, c := range r.components {
		if c.Name() == name {
			return c
		}
	}
	return nil
}
