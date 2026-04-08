package builder

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadSuccess(t *testing.T) {
	body := "hello world content"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	dl := &Downloader{Client: srv.Client()}
	dest := filepath.Join(t.TempDir(), "output.txt")

	n, err := dl.Download(context.Background(), srv.URL+"/file.txt", dest)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if n != int64(len(body)) {
		t.Errorf("bytes = %d, want %d", n, len(body))
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Errorf("content = %q, want %q", got, body)
	}
}

func TestDownloadNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	dl := &Downloader{Client: srv.Client()}
	dest := filepath.Join(t.TempDir(), "output.txt")

	_, err := dl.Download(context.Background(), srv.URL+"/missing", dest)
	if err == nil {
		t.Fatal("expected error for 404")
	}
}

func TestDownloadContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "data")
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	dl := &Downloader{Client: srv.Client()}
	dest := filepath.Join(t.TempDir(), "output.txt")

	_, err := dl.Download(ctx, srv.URL+"/file.txt", dest)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestDownloadCreatesParentDirs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "nested")
	}))
	defer srv.Close()

	dl := &Downloader{Client: srv.Client()}
	dest := filepath.Join(t.TempDir(), "a", "b", "c", "output.txt")

	_, err := dl.Download(context.Background(), srv.URL+"/file.txt", dest)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "nested" {
		t.Errorf("content = %q, want %q", got, "nested")
	}
}
