package builder

import (
	"context"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/random"
)

// startTestRegistry launches an in-memory OCI registry for the duration
// of a test and returns its host (e.g. "127.0.0.1:45871").
func startTestRegistry(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(registry.New())
	t.Cleanup(srv.Close)

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Host
}

// pushRandomImage creates a random image and pushes it to the given
// in-memory registry at the given reference.
func pushRandomImage(t *testing.T, ref string) {
	t.Helper()
	img, err := random.Image(1024, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := crane.Push(img, ref); err != nil {
		t.Fatalf("crane.Push(%s): %v", ref, err)
	}
}

func TestSanitizeImageRef(t *testing.T) {
	cases := map[string]string{
		"ghcr.io/omec-project/amf:rel-1.8.0": "ghcr.io_omec-project_amf_rel-1.8.0",
		"docker.io/library/nginx:latest":     "docker.io_library_nginx_latest",
		"registry.k8s.io/pause@sha256:abc":   "registry.k8s.io_pause_sha256_abc",
		"ghcr.io/app/foo/bar:tag":            "ghcr.io_app_foo_bar_tag",
	}
	for in, want := range cases {
		if got := sanitizeImageRef(in); got != want {
			t.Errorf("sanitizeImageRef(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDedupeAndSort(t *testing.T) {
	in := []string{
		"ghcr.io/b:v1",
		"ghcr.io/a:v1",
		"ghcr.io/b:v1",
		"  ",
		"ghcr.io/a:v1",
	}
	got := dedupeAndSort(in)
	want := []string{"ghcr.io/a:v1", "ghcr.io/b:v1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestBuildImages_EmptyInputReturnsNil(t *testing.T) {
	entry, err := BuildImages(context.Background(), nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if entry != nil {
		t.Errorf("empty input should return nil entry, got %+v", entry)
	}
}

func TestBuildImages_PullsFromTestRegistry(t *testing.T) {
	host := startTestRegistry(t)

	refs := []string{
		host + "/alpha/app:v1",
		host + "/beta/app:v2",
	}
	for _, r := range refs {
		pushRandomImage(t, r)
	}

	stageDir := t.TempDir()
	entry, err := BuildImages(context.Background(), refs, stageDir)
	if err != nil {
		t.Fatalf("BuildImages: %v", err)
	}
	if entry == nil {
		t.Fatal("entry is nil")
	}
	if len(entry.Images) != 2 {
		t.Fatalf("len(Images) = %d, want 2", len(entry.Images))
	}

	for _, art := range entry.Images {
		if !strings.HasPrefix(art.Digest, "sha256:") {
			t.Errorf("digest %q should start with sha256:", art.Digest)
		}
		if art.SHA256 == "" {
			t.Errorf("SHA256 is empty for %s", art.Ref)
		}
		if art.Size <= 0 {
			t.Errorf("Size = %d for %s", art.Size, art.Ref)
		}
		tarPath := filepath.Join(stageDir, art.Path)
		if _, err := os.Stat(tarPath); err != nil {
			t.Errorf("tarball missing at %s: %v", tarPath, err)
		}
	}
}

func TestBuildImages_DedupesInputBeforePulling(t *testing.T) {
	host := startTestRegistry(t)
	ref := host + "/only/app:v1"
	pushRandomImage(t, ref)

	entry, err := BuildImages(context.Background(), []string{ref, ref, ref}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(entry.Images) != 1 {
		t.Errorf("len(Images) = %d, want 1 (deduped)", len(entry.Images))
	}
}

func TestBuildImages_ReportsPullErrors(t *testing.T) {
	host := startTestRegistry(t)
	// Note: image was not pushed, so the pull must fail.
	refs := []string{host + "/missing/app:v1"}

	_, err := BuildImages(context.Background(), refs, t.TempDir())
	if err == nil {
		t.Fatal("expected pull error for missing image")
	}
	if !strings.Contains(err.Error(), "pulling") {
		t.Errorf("error message should mention pulling: %v", err)
	}
}
