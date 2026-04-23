package onramp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const upstreamHostsIni = `[all]
# Optional for password-based access:
# node1 ansible_host=127.0.0.1 ansible_user=<username> ansible_password=<password> ansible_sudo_pass=<sudo-password>
node1 ansible_host=127.0.0.1 ansible_user=aether
#node2 ansible_host=10.76.28.115 ansible_user=aether
#node3 ansible_host=10.76.28.117 ansible_user=aether

[master_nodes]
node1

[worker_nodes]
#node2
`

func writeHostsIni(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "hosts.ini")
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatalf("write seed hosts.ini: %v", err)
	}
	return path
}

func TestSetInventoryCredentials_StampsPasswordOnNodeLine(t *testing.T) {
	path := writeHostsIni(t, upstreamHostsIni)
	if err := setInventoryCredentials(path, "aether", "s3cret"); err != nil {
		t.Fatalf("setInventoryCredentials: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(got), "node1 ansible_host=127.0.0.1 ansible_user=aether ansible_password=s3cret") {
		t.Errorf("node1 line did not gain ansible_password:\n%s", got)
	}
}

func TestSetInventoryCredentials_LeavesCommentedGuidanceAlone(t *testing.T) {
	path := writeHostsIni(t, upstreamHostsIni)
	if err := setInventoryCredentials(path, "aether", "s3cret"); err != nil {
		t.Fatalf("setInventoryCredentials: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// The "# node1 ansible_host=..." teaching example must not have
	// the fake <username>/<password> placeholders replaced — that
	// would confuse operators later.
	if !strings.Contains(string(got), "# node1 ansible_host=127.0.0.1 ansible_user=<username>") {
		t.Errorf("commented example line was modified:\n%s", got)
	}
	// Commented alternate-node lines should also be untouched.
	if !strings.Contains(string(got), "#node2 ansible_host=10.76.28.115 ansible_user=aether\n") {
		t.Errorf("commented node2 line was modified:\n%s", got)
	}
}

func TestSetInventoryCredentials_Idempotent(t *testing.T) {
	path := writeHostsIni(t, upstreamHostsIni)
	if err := setInventoryCredentials(path, "aether", "s3cret"); err != nil {
		t.Fatalf("first call: %v", err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := setInventoryCredentials(path, "aether", "s3cret"); err != nil {
		t.Fatalf("second call: %v", err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Errorf("second call mutated file:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestSetInventoryCredentials_ReplacesExistingValues(t *testing.T) {
	seed := "[all]\nnode1 ansible_host=127.0.0.1 ansible_user=old ansible_password=old-pass\n"
	path := writeHostsIni(t, seed)
	if err := setInventoryCredentials(path, "aether", "new-pass"); err != nil {
		t.Fatalf("setInventoryCredentials: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "ansible_user=old") {
		t.Errorf("old user value leaked:\n%s", got)
	}
	if strings.Contains(string(got), "ansible_password=old-pass") {
		t.Errorf("old password value leaked:\n%s", got)
	}
	if !strings.Contains(string(got), "ansible_user=aether") || !strings.Contains(string(got), "ansible_password=new-pass") {
		t.Errorf("new creds not stamped in:\n%s", got)
	}
}

func TestSetInventoryCredentials_QuotesPasswordWithSpecials(t *testing.T) {
	path := writeHostsIni(t, upstreamHostsIni)
	if err := setInventoryCredentials(path, "aether", "sp ace#hash"); err != nil {
		t.Fatalf("setInventoryCredentials: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Ansible inventory treats '#' as the start of a comment and
	// whitespace as a key separator. The quoter must wrap such values
	// so the whole password survives as one token.
	if !strings.Contains(string(got), "ansible_password='sp ace#hash'") {
		t.Errorf("password with spaces/# should be single-quoted:\n%s", got)
	}
}
