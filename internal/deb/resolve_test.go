package deb

import "testing"

func makeTestPackages() []Package {
	return []Package{
		{
			Name: "app", Version: "1.0", Arch: "amd64",
			Depends:  ParseDependsField("libfoo (>= 1.0), libbar"),
			Filename: "pool/main/a/app/app_1.0_amd64.deb", SHA256: "aaa",
		},
		{
			Name: "libfoo", Version: "2.0", Arch: "amd64",
			Depends:  ParseDependsField("libcommon"),
			Filename: "pool/main/libf/libfoo/libfoo_2.0_amd64.deb", SHA256: "bbb",
		},
		{
			Name: "libbar", Version: "1.5", Arch: "amd64",
			Depends:  ParseDependsField("libcommon"),
			Filename: "pool/main/libb/libbar/libbar_1.5_amd64.deb", SHA256: "ccc",
		},
		{
			Name: "libcommon", Version: "3.0", Arch: "amd64",
			Filename: "pool/main/libc/libcommon/libcommon_3.0_amd64.deb", SHA256: "ddd",
		},
		{
			Name: "dpkg", Version: "1.22.6", Arch: "amd64",
			Essential: true, Priority: "required",
			Filename: "pool/main/d/dpkg/dpkg_1.22.6_amd64.deb", SHA256: "eee",
		},
		{
			Name: "alt-a", Version: "1.0", Arch: "amd64",
			Filename: "pool/main/a/alt-a/alt-a_1.0_amd64.deb", SHA256: "fff",
		},
		{
			Name: "alt-b", Version: "1.0", Arch: "amd64",
			Filename: "pool/main/a/alt-b/alt-b_1.0_amd64.deb", SHA256: "ggg",
		},
		{
			Name: "with-alts", Version: "1.0", Arch: "amd64",
			Depends:  ParseDependsField("alt-a | alt-b"),
			Filename: "pool/main/w/with-alts/with-alts_1.0_amd64.deb", SHA256: "hhh",
		},
		{
			Name: "with-essential-dep", Version: "1.0", Arch: "amd64",
			Depends:  ParseDependsField("dpkg (>= 1.0)"),
			Filename: "pool/main/w/with-essential-dep/with-essential-dep_1.0_amd64.deb", SHA256: "iii",
		},
		// Cycle: cycle-a depends on cycle-b, cycle-b depends on cycle-a.
		{
			Name: "cycle-a", Version: "1.0", Arch: "amd64",
			Depends:  ParseDependsField("cycle-b"),
			Filename: "pool/main/c/cycle-a/cycle-a_1.0_amd64.deb", SHA256: "jjj",
		},
		{
			Name: "cycle-b", Version: "1.0", Arch: "amd64",
			Depends:  ParseDependsField("cycle-a"),
			Filename: "pool/main/c/cycle-b/cycle-b_1.0_amd64.deb", SHA256: "kkk",
		},
	}
}

func TestResolveDiamondDependency(t *testing.T) {
	idx := NewIndex(makeTestPackages())
	result, err := Resolve([]string{"app"}, idx, nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	names := map[string]bool{}
	for _, p := range result {
		names[p.Name] = true
	}

	for _, want := range []string{"app", "libfoo", "libbar", "libcommon"} {
		if !names[want] {
			t.Errorf("missing package %q in result", want)
		}
	}

	// dpkg should NOT be in the result (Essential/required).
	if names["dpkg"] {
		t.Error("dpkg (Essential) should be skipped")
	}
}

func TestResolveAlternatives(t *testing.T) {
	idx := NewIndex(makeTestPackages())
	result, err := Resolve([]string{"with-alts"}, idx, nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	names := map[string]bool{}
	for _, p := range result {
		names[p.Name] = true
	}

	if !names["with-alts"] {
		t.Error("missing with-alts")
	}
	// First alternative should be picked.
	if !names["alt-a"] {
		t.Error("should pick first alternative alt-a")
	}
	// Second alternative should NOT be included.
	if names["alt-b"] {
		t.Error("alt-b should not be included (alt-a was picked)")
	}
}

func TestResolveEssentialSkipped(t *testing.T) {
	idx := NewIndex(makeTestPackages())
	result, err := Resolve([]string{"with-essential-dep"}, idx, nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	names := map[string]bool{}
	for _, p := range result {
		names[p.Name] = true
	}

	if !names["with-essential-dep"] {
		t.Error("missing with-essential-dep")
	}
	if names["dpkg"] {
		t.Error("dpkg should be skipped (Essential)")
	}
}

func TestResolveCycleTerminates(t *testing.T) {
	idx := NewIndex(makeTestPackages())
	result, err := Resolve([]string{"cycle-a"}, idx, nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	names := map[string]bool{}
	for _, p := range result {
		names[p.Name] = true
	}

	if !names["cycle-a"] || !names["cycle-b"] {
		t.Error("both cycle packages should be resolved")
	}
	if len(result) != 2 {
		t.Errorf("len(result) = %d, want 2", len(result))
	}
}

func TestResolveMissingPackage(t *testing.T) {
	idx := NewIndex(makeTestPackages())
	_, err := Resolve([]string{"nonexistent"}, idx, nil)
	if err == nil {
		t.Fatal("should error on missing package")
	}
}

func TestResolveVersionConstraint(t *testing.T) {
	idx := NewIndex(makeTestPackages())
	constraints := map[string]Constraint{
		"libfoo": {Op: ">=", Version: "2.0"},
	}
	result, err := Resolve([]string{"libfoo"}, idx, constraints)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(result) == 0 {
		t.Fatal("expected at least one package")
	}
}

func TestResolveVersionConstraintFails(t *testing.T) {
	idx := NewIndex(makeTestPackages())
	constraints := map[string]Constraint{
		"libfoo": {Op: ">=", Version: "99.0"},
	}
	_, err := Resolve([]string{"libfoo"}, idx, constraints)
	if err == nil {
		t.Fatal("should fail when version constraint unsatisfied")
	}
}

func TestIndexHighestVersion(t *testing.T) {
	pkgs := []Package{
		{Name: "foo", Version: "1.0"},
		{Name: "foo", Version: "2.0"},
		{Name: "foo", Version: "1.5"},
	}
	idx := NewIndex(pkgs)
	p := idx.Lookup("foo")
	if p == nil {
		t.Fatal("foo not found")
	}
	if p.Version != "2.0" {
		t.Errorf("Version = %q, want 2.0 (highest)", p.Version)
	}
}

func TestIndexVirtualPackage(t *testing.T) {
	pkgs := []Package{
		{Name: "postfix", Version: "1.0", Provides: []string{"mail-transport-agent"}},
	}
	idx := NewIndex(pkgs)
	p := idx.Lookup("mail-transport-agent")
	if p == nil {
		t.Fatal("virtual package not resolved")
	}
	if p.Name != "postfix" {
		t.Errorf("resolved to %q, want postfix", p.Name)
	}
}
