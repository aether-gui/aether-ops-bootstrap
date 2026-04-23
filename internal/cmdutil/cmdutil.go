// Package cmdutil wraps os/exec so the launcher can toggle live subprocess
// output (--verbose) without every call site carrying its own plumbing.
//
// Verbosity is carried on context via WithVerbose / IsVerbose. Run wraps
// *exec.Cmd.Run so the returned bytes match what CombinedOutput would have
// returned, while optionally teeing output to os.Stderr in real time.
package cmdutil

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
)

type verboseKey struct{}

// WithVerbose returns a context that reports the given verbose setting.
func WithVerbose(ctx context.Context, verbose bool) context.Context {
	return context.WithValue(ctx, verboseKey{}, verbose)
}

// IsVerbose reports whether the context was marked verbose.
func IsVerbose(ctx context.Context) bool {
	v, _ := ctx.Value(verboseKey{}).(bool)
	return v
}

// Run executes cmd and returns its combined stdout+stderr. When the context
// is verbose the output is also streamed to os.Stderr as it is produced, so
// long-running subprocesses (dpkg, helm, etc.) give live feedback without
// losing the captured bytes used for error reporting.
//
// The caller may set cmd.Stdin before calling. cmd.Stdout/Stderr are
// overwritten by Run.
func Run(ctx context.Context, cmd *exec.Cmd) ([]byte, error) {
	var buf bytes.Buffer
	if IsVerbose(ctx) {
		cmd.Stdout = io.MultiWriter(os.Stderr, &buf)
		cmd.Stderr = io.MultiWriter(os.Stderr, &buf)
	} else {
		cmd.Stdout = &buf
		cmd.Stderr = &buf
	}
	err := cmd.Run()
	return buf.Bytes(), err
}
