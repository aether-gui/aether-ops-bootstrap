package systemd

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
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

// SystemctlManager implements Manager by executing systemctl commands.
// This is simpler than D-Bus and systemctl is always available on
// systems with systemd.
type SystemctlManager struct{}

func (s *SystemctlManager) DaemonReload(ctx context.Context) error {
	return runSystemctl(ctx, "daemon-reload")
}

func (s *SystemctlManager) Start(ctx context.Context, unit string) error {
	return runSystemctl(ctx, "start", unit)
}

func (s *SystemctlManager) Stop(ctx context.Context, unit string) error {
	return runSystemctl(ctx, "stop", unit)
}

func (s *SystemctlManager) Enable(ctx context.Context, unit string) error {
	return runSystemctl(ctx, "enable", unit)
}

func (s *SystemctlManager) Status(ctx context.Context, unit string) (UnitStatus, error) {
	cmd := exec.CommandContext(ctx, "systemctl", "show", unit, "--property=ActiveState,SubState", "--no-pager")
	output, err := cmd.Output()
	if err != nil {
		return UnitStatus{}, fmt.Errorf("systemctl show %s: %w", unit, err)
	}

	status := UnitStatus{Name: unit}
	for _, line := range strings.Split(string(output), "\n") {
		if strings.HasPrefix(line, "ActiveState=") {
			status.ActiveState = strings.TrimPrefix(line, "ActiveState=")
		}
		if strings.HasPrefix(line, "SubState=") {
			status.SubState = strings.TrimPrefix(line, "SubState=")
		}
	}
	return status, nil
}

func runSystemctl(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "systemctl", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s: %w\n%s", strings.Join(args, " "), err, output)
	}
	return nil
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
