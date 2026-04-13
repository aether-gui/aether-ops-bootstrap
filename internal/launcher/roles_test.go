package launcher

import (
	"reflect"
	"testing"
)

func TestNormalizeRole_Canonical(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  Role
	}{
		{"mgmt", RoleMgmt},
		{"core", RoleCore},
		{"ran", RoleRan},
	} {
		got, err := NormalizeRole(tc.input)
		if err != nil {
			t.Errorf("NormalizeRole(%q): %v", tc.input, err)
		}
		if got != tc.want {
			t.Errorf("NormalizeRole(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestNormalizeRole_Aliases(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  Role
	}{
		{"management", RoleMgmt},
		{"MANAGEMENT", RoleMgmt},
		{"sd-core", RoleCore},
		{"SD-Core", RoleCore},
		{"srs-ran", RoleRan},
		{"ocudu", RoleRan},
		{"OCUDU", RoleRan},
	} {
		got, err := NormalizeRole(tc.input)
		if err != nil {
			t.Errorf("NormalizeRole(%q): %v", tc.input, err)
		}
		if got != tc.want {
			t.Errorf("NormalizeRole(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestNormalizeRole_Unknown(t *testing.T) {
	_, err := NormalizeRole("unknown")
	if err == nil {
		t.Fatal("expected error for unknown role")
	}
}

func TestNormalizeRole_Whitespace(t *testing.T) {
	got, err := NormalizeRole("  mgmt  ")
	if err != nil {
		t.Fatal(err)
	}
	if got != RoleMgmt {
		t.Errorf("got %q, want %q", got, RoleMgmt)
	}
}

func TestParseRoles_Single(t *testing.T) {
	got, err := ParseRoles("mgmt")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []Role{RoleMgmt}) {
		t.Errorf("got %v", got)
	}
}

func TestParseRoles_Multiple(t *testing.T) {
	got, err := ParseRoles("core,mgmt")
	if err != nil {
		t.Fatal(err)
	}
	// Sorted: core < mgmt.
	want := []Role{RoleCore, RoleMgmt}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseRoles_Aliases(t *testing.T) {
	got, err := ParseRoles("management,sd-core,ocudu")
	if err != nil {
		t.Fatal(err)
	}
	want := []Role{RoleCore, RoleMgmt, RoleRan}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseRoles_Deduplicates(t *testing.T) {
	got, err := ParseRoles("mgmt,management,mgmt")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != RoleMgmt {
		t.Errorf("got %v, want [mgmt]", got)
	}
}

func TestParseRoles_Empty(t *testing.T) {
	got, err := ParseRoles("")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

func TestParseRoles_UnknownError(t *testing.T) {
	_, err := ParseRoles("mgmt,bogus")
	if err == nil {
		t.Fatal("expected error for unknown role")
	}
}

func TestComponentsForRoles_Mgmt(t *testing.T) {
	got := ComponentsForRoles([]Role{RoleMgmt})
	for _, name := range []string{"debs", "ssh", "sudoers", "service_account", "onramp", "aether_ops"} {
		if !got[name] {
			t.Errorf("mgmt should include %q", name)
		}
	}
	for _, name := range []string{"rke2", "helm"} {
		if got[name] {
			t.Errorf("mgmt should NOT include %q", name)
		}
	}
}

func TestComponentsForRoles_Core(t *testing.T) {
	got := ComponentsForRoles([]Role{RoleCore})
	for _, name := range []string{"debs", "ssh", "sudoers", "service_account", "rke2", "helm"} {
		if !got[name] {
			t.Errorf("core should include %q", name)
		}
	}
	for _, name := range []string{"onramp", "aether_ops"} {
		if got[name] {
			t.Errorf("core should NOT include %q", name)
		}
	}
}

func TestComponentsForRoles_Ran(t *testing.T) {
	got := ComponentsForRoles([]Role{RoleRan})
	for _, name := range []string{"debs", "ssh", "sudoers", "service_account"} {
		if !got[name] {
			t.Errorf("ran should include %q", name)
		}
	}
	if len(got) != 4 {
		t.Errorf("ran should have exactly 4 components, got %d", len(got))
	}
}

func TestComponentsForRoles_Union(t *testing.T) {
	got := ComponentsForRoles([]Role{RoleMgmt, RoleCore})
	// Union should include everything from both roles.
	for _, name := range []string{"debs", "ssh", "sudoers", "service_account", "rke2", "helm", "onramp", "aether_ops"} {
		if !got[name] {
			t.Errorf("mgmt+core union should include %q", name)
		}
	}
}

func TestRoleStrings(t *testing.T) {
	got := RoleStrings([]Role{RoleMgmt, RoleCore})
	want := []string{"mgmt", "core"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseRoleStrings(t *testing.T) {
	got, err := ParseRoleStrings([]string{"mgmt", "core"})
	if err != nil {
		t.Fatal(err)
	}
	want := []Role{RoleMgmt, RoleCore}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseRoleStrings_Invalid(t *testing.T) {
	_, err := ParseRoleStrings([]string{"mgmt", "bogus"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestContainsRole(t *testing.T) {
	roles := []Role{RoleMgmt, RoleCore}
	if !ContainsRole(roles, RoleMgmt) {
		t.Error("should contain mgmt")
	}
	if ContainsRole(roles, RoleRan) {
		t.Error("should not contain ran")
	}
}
