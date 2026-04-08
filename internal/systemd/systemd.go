package systemd

import (
	"context"
	"errors"
)

// ErrNotImplemented is returned by stub implementations.
var ErrNotImplemented = errors.New("not implemented")

// UnitStatus describes the current state of a systemd unit.
type UnitStatus struct {
	Name        string `json:"name"`
	ActiveState string `json:"active_state"`
	SubState    string `json:"sub_state"`
}

// Manager controls systemd units. The interface allows swapping real D-Bus
// calls for a mock in tests.
type Manager interface {
	DaemonReload(ctx context.Context) error
	Start(ctx context.Context, unit string) error
	Stop(ctx context.Context, unit string) error
	Enable(ctx context.Context, unit string) error
	Status(ctx context.Context, unit string) (UnitStatus, error)
}

// DBusManager implements Manager using the system D-Bus connection.
// Currently a stub; the real implementation will use go-systemd/dbus.
type DBusManager struct{}

func (d *DBusManager) DaemonReload(ctx context.Context) error {
	return ErrNotImplemented
}

func (d *DBusManager) Start(ctx context.Context, unit string) error {
	return ErrNotImplemented
}

func (d *DBusManager) Stop(ctx context.Context, unit string) error {
	return ErrNotImplemented
}

func (d *DBusManager) Enable(ctx context.Context, unit string) error {
	return ErrNotImplemented
}

func (d *DBusManager) Status(ctx context.Context, unit string) (UnitStatus, error) {
	return UnitStatus{}, ErrNotImplemented
}

// MockManager records calls for testing. Each method appends to Calls
// and returns the error configured in the corresponding Err field.
type MockManager struct {
	Calls           []MockCall
	DaemonReloadErr error
	StartErr        error
	StopErr         error
	EnableErr       error
	StatusErr       error
	StatusResult    UnitStatus
}

// MockCall records a single method invocation on MockManager.
type MockCall struct {
	Method string
	Unit   string
}

func (m *MockManager) DaemonReload(ctx context.Context) error {
	m.Calls = append(m.Calls, MockCall{Method: "DaemonReload"})
	return m.DaemonReloadErr
}

func (m *MockManager) Start(ctx context.Context, unit string) error {
	m.Calls = append(m.Calls, MockCall{Method: "Start", Unit: unit})
	return m.StartErr
}

func (m *MockManager) Stop(ctx context.Context, unit string) error {
	m.Calls = append(m.Calls, MockCall{Method: "Stop", Unit: unit})
	return m.StopErr
}

func (m *MockManager) Enable(ctx context.Context, unit string) error {
	m.Calls = append(m.Calls, MockCall{Method: "Enable", Unit: unit})
	return m.EnableErr
}

func (m *MockManager) Status(ctx context.Context, unit string) (UnitStatus, error) {
	m.Calls = append(m.Calls, MockCall{Method: "Status", Unit: unit})
	return m.StatusResult, m.StatusErr
}
