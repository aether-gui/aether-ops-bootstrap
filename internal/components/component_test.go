package components

import (
	"context"
	"testing"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
	"github.com/aether-gui/aether-ops-bootstrap/internal/state"
)

// stubComponent is a minimal Component for testing the registry.
type stubComponent struct {
	name string
}

func (s *stubComponent) Name() string                             { return s.name }
func (s *stubComponent) DesiredVersion(_ *bundle.Manifest) string { return "" }
func (s *stubComponent) CurrentVersion(_ *state.State) string     { return "" }
func (s *stubComponent) Plan(_, _ string) (Plan, error)           { return Plan{NoOp: true}, nil }
func (s *stubComponent) Apply(_ context.Context, _ Plan) error    { return nil }

func TestRegistryRegisterAndAll(t *testing.T) {
	r := &Registry{}
	r.Register(&stubComponent{name: "a"})
	r.Register(&stubComponent{name: "b"})
	r.Register(&stubComponent{name: "c"})

	all := r.All()
	if len(all) != 3 {
		t.Fatalf("len(All) = %d, want 3", len(all))
	}

	// Order is preserved.
	names := []string{"a", "b", "c"}
	for i, c := range all {
		if c.Name() != names[i] {
			t.Errorf("All[%d].Name() = %q, want %q", i, c.Name(), names[i])
		}
	}
}

func TestRegistryByName(t *testing.T) {
	r := &Registry{}
	r.Register(&stubComponent{name: "debs"})
	r.Register(&stubComponent{name: "rke2"})

	c := r.ByName("rke2")
	if c == nil {
		t.Fatal("ByName(rke2) = nil, want non-nil")
	}
	if c.Name() != "rke2" {
		t.Errorf("Name() = %q, want %q", c.Name(), "rke2")
	}

	if r.ByName("nonexistent") != nil {
		t.Error("ByName(nonexistent) should return nil")
	}
}

func TestRegistryAllReturnsCopy(t *testing.T) {
	r := &Registry{}
	r.Register(&stubComponent{name: "a"})

	all := r.All()
	all[0] = &stubComponent{name: "modified"}

	// Original registry should be unaffected.
	if r.All()[0].Name() != "a" {
		t.Error("All() should return a copy, not a reference to internal slice")
	}
}

func TestRegistryFilter(t *testing.T) {
	r := &Registry{}
	r.Register(&stubComponent{name: "debs"})
	r.Register(&stubComponent{name: "ssh"})
	r.Register(&stubComponent{name: "rke2"})
	r.Register(&stubComponent{name: "aether_ops"})

	allowed := map[string]bool{"debs": true, "rke2": true}
	filtered := r.Filter(allowed)

	all := filtered.All()
	if len(all) != 2 {
		t.Fatalf("len(filtered) = %d, want 2", len(all))
	}
	if all[0].Name() != "debs" || all[1].Name() != "rke2" {
		t.Errorf("filtered = [%s, %s], want [debs, rke2]", all[0].Name(), all[1].Name())
	}

	// Original registry unaffected.
	if len(r.All()) != 4 {
		t.Error("original registry should be unchanged")
	}
}

func TestRegistryFilterEmpty(t *testing.T) {
	r := &Registry{}
	r.Register(&stubComponent{name: "debs"})

	filtered := r.Filter(map[string]bool{"nonexistent": true})
	if len(filtered.All()) != 0 {
		t.Errorf("expected empty filtered registry")
	}
}

func TestPlanNoOp(t *testing.T) {
	p := Plan{NoOp: true}
	if !p.NoOp {
		t.Error("NoOp should be true")
	}
	if len(p.Actions) != 0 {
		t.Error("NoOp plan should have no actions")
	}
}

func TestPlanActionsExecuteInOrder(t *testing.T) {
	var order []int
	p := Plan{
		Actions: []Action{
			{Description: "first", Fn: func(_ context.Context) error { order = append(order, 1); return nil }},
			{Description: "second", Fn: func(_ context.Context) error { order = append(order, 2); return nil }},
			{Description: "third", Fn: func(_ context.Context) error { order = append(order, 3); return nil }},
		},
	}

	ctx := context.Background()
	for _, a := range p.Actions {
		if err := a.Fn(ctx); err != nil {
			t.Fatalf("action %q: %v", a.Description, err)
		}
	}

	if len(order) != 3 {
		t.Fatalf("len(order) = %d, want 3", len(order))
	}
	for i, v := range order {
		if v != i+1 {
			t.Errorf("order[%d] = %d, want %d", i, v, i+1)
		}
	}
}
