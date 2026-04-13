// Package launcher provides the bootstrap install orchestration.
//
// This file defines the role-based component filtering system. Roles are
// a transitional mechanism that lets a single bundle provision different
// node types (management, SD-Core, RAN) by selecting which components to
// apply at install time. Long-term, per-role bundle specs will replace
// this flag — see MULTI-NODE-DESIGN.md for the full roadmap.
package launcher

import (
	"fmt"
	"sort"
	"strings"
)

// Role represents a node role in a multi-node deployment.
type Role string

const (
	RoleMgmt Role = "mgmt"
	RoleCore Role = "core"
	RoleRan  Role = "ran"
)

// AllRoles lists every valid canonical role name.
var AllRoles = []Role{RoleMgmt, RoleCore, RoleRan}

// roleAliases maps user-facing names (including canonical names) to
// their canonical Role values.
var roleAliases = map[string]Role{
	"mgmt":       RoleMgmt,
	"management": RoleMgmt,
	"core":       RoleCore,
	"sd-core":    RoleCore,
	"ran":        RoleRan,
	"srs-ran":    RoleRan,
	"ocudu":      RoleRan,
}

// roleComponents maps each role to the component names it requires.
// When multiple roles are requested, the launcher takes the union.
// When no roles are specified, all registered components run (backward
// compatible single-node mode).
var roleComponents = map[Role][]string{
	RoleMgmt: {"debs", "ssh", "sudoers", "service_account", "onramp", "aether_ops"},
	RoleCore: {"debs", "ssh", "sudoers", "service_account", "rke2", "helm"},
	RoleRan:  {"debs", "ssh", "sudoers", "service_account"},
}

// NormalizeRole converts a user-provided string to a canonical Role.
// The input is lowercased and looked up in the alias table.
func NormalizeRole(s string) (Role, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if r, ok := roleAliases[s]; ok {
		return r, nil
	}
	valid := make([]string, 0, len(AllRoles))
	for _, r := range AllRoles {
		valid = append(valid, string(r))
	}
	return "", fmt.Errorf("unknown role %q (valid: %s)", s, strings.Join(valid, ", "))
}

// ParseRoles splits a comma-separated string into canonical roles.
// Duplicates are removed and the result is sorted for deterministic
// state recording.
func ParseRoles(csv string) ([]Role, error) {
	csv = strings.TrimSpace(csv)
	if csv == "" {
		return nil, nil
	}

	seen := map[Role]bool{}
	var out []Role
	for _, tok := range strings.Split(csv, ",") {
		r, err := NormalizeRole(tok)
		if err != nil {
			return nil, err
		}
		if seen[r] {
			continue
		}
		seen[r] = true
		out = append(out, r)
	}

	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

// ComponentsForRoles returns the union of component names required by
// the given roles. The returned map has O(1) lookup.
func ComponentsForRoles(roles []Role) map[string]bool {
	out := map[string]bool{}
	for _, r := range roles {
		for _, name := range roleComponents[r] {
			out[name] = true
		}
	}
	return out
}

// RoleStrings converts a slice of Roles to strings for state recording.
func RoleStrings(roles []Role) []string {
	out := make([]string, len(roles))
	for i, r := range roles {
		out[i] = string(r)
	}
	return out
}

// ParseRoleStrings converts stored string roles back to typed Roles.
func ParseRoleStrings(ss []string) ([]Role, error) {
	var out []Role
	for _, s := range ss {
		r, err := NormalizeRole(s)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// ContainsRole reports whether the given role is in the slice.
func ContainsRole(roles []Role, target Role) bool {
	for _, r := range roles {
		if r == target {
			return true
		}
	}
	return false
}
