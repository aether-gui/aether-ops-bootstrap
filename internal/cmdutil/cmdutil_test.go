package cmdutil

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestIsVerbose_DefaultFalse(t *testing.T) {
	if IsVerbose(context.Background()) {
		t.Fatal("IsVerbose on a bare context should report false")
	}
}

func TestIsVerbose_Roundtrip(t *testing.T) {
	ctx := WithVerbose(context.Background(), true)
	if !IsVerbose(ctx) {
		t.Fatal("IsVerbose should report true after WithVerbose(ctx, true)")
	}
	ctx = WithVerbose(ctx, false)
	if IsVerbose(ctx) {
		t.Fatal("IsVerbose should report false after WithVerbose(ctx, false)")
	}
}

func TestRun_CapturesOutputNonVerbose(t *testing.T) {
	cmd := exec.Command("sh", "-c", "echo hello; echo err 1>&2")
	out, err := Run(context.Background(), cmd)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, "hello") || !strings.Contains(got, "err") {
		t.Fatalf("expected combined stdout+stderr, got %q", got)
	}
}

func TestRun_ReturnsErrorOnFailure(t *testing.T) {
	cmd := exec.Command("sh", "-c", "echo boom 1>&2; exit 3")
	out, err := Run(context.Background(), cmd)
	if err == nil {
		t.Fatal("Run should return error when command exits non-zero")
	}
	if !strings.Contains(string(out), "boom") {
		t.Fatalf("output should include stderr on failure, got %q", string(out))
	}
}

func TestRun_VerboseTeesToStderr(t *testing.T) {
	// Redirect os.Stderr to a pipe so the test can observe what Run writes
	// to it. Caddy-style subprocess live output lives on os.Stderr; this
	// exercise confirms the tee path.
	origStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	defer func() { os.Stderr = origStderr }()

	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()

	ctx := WithVerbose(context.Background(), true)
	cmd := exec.Command("sh", "-c", "echo streamed")
	out, runErr := Run(ctx, cmd)
	w.Close()
	streamed := <-done
	os.Stderr = origStderr

	if runErr != nil {
		t.Fatalf("Run: %v", runErr)
	}
	if !strings.Contains(string(out), "streamed") {
		t.Fatalf("returned bytes should still include output, got %q", string(out))
	}
	if !strings.Contains(streamed, "streamed") {
		t.Fatalf("verbose mode should tee to os.Stderr, stderr got %q", streamed)
	}
}
