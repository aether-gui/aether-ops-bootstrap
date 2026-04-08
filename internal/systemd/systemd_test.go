package systemd

import (
	"context"
	"errors"
	"testing"
)

// Compile-time interface check.
var _ Manager = (*DBusManager)(nil)
var _ Manager = (*MockManager)(nil)

func TestMockManagerRecordsCalls(t *testing.T) {
	ctx := context.Background()
	m := &MockManager{}

	m.DaemonReload(ctx)
	m.Start(ctx, "rke2-server.service")
	m.Enable(ctx, "rke2-server.service")
	m.Stop(ctx, "rke2-server.service")
	m.Status(ctx, "rke2-server.service")

	want := []MockCall{
		{Method: "DaemonReload"},
		{Method: "Start", Unit: "rke2-server.service"},
		{Method: "Enable", Unit: "rke2-server.service"},
		{Method: "Stop", Unit: "rke2-server.service"},
		{Method: "Status", Unit: "rke2-server.service"},
	}

	if len(m.Calls) != len(want) {
		t.Fatalf("len(Calls) = %d, want %d", len(m.Calls), len(want))
	}

	for i, c := range m.Calls {
		if c.Method != want[i].Method || c.Unit != want[i].Unit {
			t.Errorf("Calls[%d] = %+v, want %+v", i, c, want[i])
		}
	}
}

func TestMockManagerReturnsConfiguredErrors(t *testing.T) {
	ctx := context.Background()
	testErr := errors.New("test error")
	m := &MockManager{StartErr: testErr}

	err := m.Start(ctx, "foo.service")
	if !errors.Is(err, testErr) {
		t.Errorf("Start error = %v, want %v", err, testErr)
	}

	// Other methods should return nil by default.
	if err := m.Stop(ctx, "foo.service"); err != nil {
		t.Errorf("Stop error = %v, want nil", err)
	}
}

func TestMockManagerStatusResult(t *testing.T) {
	ctx := context.Background()
	m := &MockManager{
		StatusResult: UnitStatus{
			Name:        "rke2-server.service",
			ActiveState: "active",
			SubState:    "running",
		},
	}

	status, err := m.Status(ctx, "rke2-server.service")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.ActiveState != "active" {
		t.Errorf("ActiveState = %q, want %q", status.ActiveState, "active")
	}
	if status.SubState != "running" {
		t.Errorf("SubState = %q, want %q", status.SubState, "running")
	}
}

func TestDBusManagerReturnsNotImplemented(t *testing.T) {
	ctx := context.Background()
	d := &DBusManager{}

	if err := d.Start(ctx, "foo.service"); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("Start error = %v, want ErrNotImplemented", err)
	}
	if err := d.Stop(ctx, "foo.service"); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("Stop error = %v, want ErrNotImplemented", err)
	}
	if err := d.Enable(ctx, "foo.service"); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("Enable error = %v, want ErrNotImplemented", err)
	}
	if err := d.DaemonReload(ctx); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("DaemonReload error = %v, want ErrNotImplemented", err)
	}
	if _, err := d.Status(ctx, "foo.service"); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("Status error = %v, want ErrNotImplemented", err)
	}
}
