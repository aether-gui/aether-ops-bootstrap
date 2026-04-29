package builder

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestScanOnrampDependencies(t *testing.T) {
	root := t.TempDir()

	writeOnrampScanTestFile(t, filepath.Join(root, "deps", "role", "tasks", "main.yml"), `
- name: install apt deps
  ansible.builtin.apt:
    name:
      - python3-pip
      - python3-venv

- name: generic package module still matters on ubuntu
  package:
    name: python3-docker

- name: inline pip names
  pip:
    name:
      - requests==2.31.0
      - urllib3==2.2.1

- name: requirements file
  ansible.builtin.pip:
    requirements: ../files/requirements.txt

- name: shell pip installs
  ansible.builtin.shell: |
    python3 -m pip install pyyaml==6.0.2
    pip install charset-normalizer==3.3.2

- name: shell pip requirements file
  command: pip install -r ../files/shell-requirements.txt

- name: loop-driven apt packages
  apt:
    name: "{{ item }}"
  loop:
    - iproute2
    - net-tools

- name: add docker repo
  copy:
    dest: /etc/apt/sources.list.d/docker.sources
    content: |
      Types: deb
      URIs: https://download.docker.com/linux/ubuntu
      Suites: noble
      Components: stable
`)

	writeOnrampScanTestFile(t, filepath.Join(root, "deps", "role", "files", "requirements.txt"), `
ansible-core==2.17.1
-r nested.txt
# comment
`)

	writeOnrampScanTestFile(t, filepath.Join(root, "deps", "role", "files", "nested.txt"), `
docker==7.1.0
`)

	writeOnrampScanTestFile(t, filepath.Join(root, "deps", "role", "files", "shell-requirements.txt"), `
certifi==2024.2.2
`)

	scan, err := ScanOnrampDependencies(root)
	if err != nil {
		t.Fatalf("ScanOnrampDependencies: %v", err)
	}

	wantApt := []string{"iproute2", "net-tools", "python3-docker", "python3-pip", "python3-venv"}
	if !reflect.DeepEqual(scan.AptPackages, wantApt) {
		t.Fatalf("AptPackages = %#v, want %#v", scan.AptPackages, wantApt)
	}
	if got := scan.AptSources["iproute2"]; len(got) != 1 || got[0] != filepath.Join(root, "deps", "role", "tasks", "main.yml") {
		t.Fatalf("AptSources[iproute2] = %#v", got)
	}
	if len(scan.AptRepositories) != 1 {
		t.Fatalf("AptRepositories = %#v, want 1 entry", scan.AptRepositories)
	}
	if scan.AptRepositories[0].Name != "docker" || scan.AptRepositories[0].URL != "https://download.docker.com/linux/ubuntu" {
		t.Fatalf("AptRepositories[0] = %#v", scan.AptRepositories[0])
	}
	if got := scan.AptRepoSources["docker"]; len(got) != 1 || got[0] != filepath.Join(root, "deps", "role", "tasks", "main.yml") {
		t.Fatalf("AptRepoSources[docker] = %#v", got)
	}

	wantPip := []string{"ansible-core==2.17.1", "certifi==2024.2.2", "charset-normalizer==3.3.2", "docker==7.1.0", "pyyaml==6.0.2", "requests==2.31.0", "urllib3==2.2.1"}
	if !reflect.DeepEqual(scan.PipRequirements, wantPip) {
		t.Fatalf("PipRequirements = %#v, want %#v", scan.PipRequirements, wantPip)
	}

	if len(scan.Unresolved) != 0 {
		t.Fatalf("Unresolved = %#v, want empty", scan.Unresolved)
	}
}

func TestScanOnrampDependenciesTracksUnresolved(t *testing.T) {
	root := t.TempDir()

	writeOnrampScanTestFile(t, filepath.Join(root, "tasks.yml"), `
- apt:
    name: "{{ runtime_pkg }}"
- pip:
    requirements: /tmp/requirements.txt
- ansible.builtin.pip:
    name: "{{ runtime_pip_pkg }}"
- shell: pip install https://example.com/pkg.whl
- apt:
    name: "{{ item }}"
  loop: "{{ packages }}"
`)

	writeOnrampScanTestFile(t, filepath.Join(root, "reqs.txt"), `
git+https://example.com/repo.git
`)

	scan, err := ScanOnrampDependencies(root)
	if err != nil {
		t.Fatalf("ScanOnrampDependencies: %v", err)
	}

	if len(scan.Unresolved) != 5 {
		t.Fatalf("len(Unresolved) = %d, want 5 (%#v)", len(scan.Unresolved), scan.Unresolved)
	}
}

func TestScanOnrampDependenciesWithItemsScalarList(t *testing.T) {
	root := t.TempDir()

	writeOnrampScanTestFile(t, filepath.Join(root, "tasks.yml"), `
- ansible.builtin.apt:
    name: "{{ item }}"
  with_items: curl, wget
`)

	scan, err := ScanOnrampDependencies(root)
	if err != nil {
		t.Fatalf("ScanOnrampDependencies: %v", err)
	}

	wantApt := []string{"curl", "wget"}
	if !reflect.DeepEqual(scan.AptPackages, wantApt) {
		t.Fatalf("AptPackages = %#v, want %#v", scan.AptPackages, wantApt)
	}
	if len(scan.Unresolved) != 0 {
		t.Fatalf("Unresolved = %#v, want empty", scan.Unresolved)
	}
}

func TestScanOnrampDependenciesIgnoresAbsentAptTasks(t *testing.T) {
	root := t.TempDir()

	writeOnrampScanTestFile(t, filepath.Join(root, "tasks.yml"), `
- name: remove conflicting docker packages
  apt:
    name:
      - docker.io
      - podman-docker
      - runc
    state: absent

- name: install docker packages
  apt:
    name:
      - docker-ce
      - docker-ce-cli
    state: present
`)

	scan, err := ScanOnrampDependencies(root)
	if err != nil {
		t.Fatalf("ScanOnrampDependencies: %v", err)
	}

	wantApt := []string{"docker-ce", "docker-ce-cli"}
	if !reflect.DeepEqual(scan.AptPackages, wantApt) {
		t.Fatalf("AptPackages = %#v, want %#v", scan.AptPackages, wantApt)
	}
	if len(scan.Unresolved) != 0 {
		t.Fatalf("Unresolved = %#v, want empty", scan.Unresolved)
	}
}

func TestScanOnrampDependenciesRequirementsOutsideRepo(t *testing.T) {
	root := t.TempDir()

	writeOnrampScanTestFile(t, filepath.Join(root, "tasks.yml"), `
- pip:
    requirements: ../outside.txt
`)

	scan, err := ScanOnrampDependencies(root)
	if err != nil {
		t.Fatalf("ScanOnrampDependencies: %v", err)
	}
	if len(scan.Unresolved) != 1 {
		t.Fatalf("len(Unresolved) = %d, want 1 (%#v)", len(scan.Unresolved), scan.Unresolved)
	}
}

func writeOnrampScanTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}
