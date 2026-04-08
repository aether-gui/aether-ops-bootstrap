package builder

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

// Downloader fetches remote artifacts over HTTP. Inject a custom
// *http.Client for testing (e.g. with httptest).
type Downloader struct {
	Client *http.Client
}

// Download fetches url and writes it to destPath. Creates parent
// directories as needed. Writes to a temp file first, then renames
// for atomicity. Returns the number of bytes written.
func (d *Downloader) Download(ctx context.Context, url, destPath string) (int64, error) {
	log.Printf("downloading %s", url)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("creating request for %s: %w", url, err)
	}

	client := d.Client
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("fetching %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("fetching %s: HTTP %d", url, resp.StatusCode)
	}

	dir := filepath.Dir(destPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return 0, fmt.Errorf("creating directory %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".download-*.tmp")
	if err != nil {
		return 0, fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()

	n, err := io.Copy(tmp, resp.Body)
	if closeErr := tmp.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(tmpPath)
		return 0, fmt.Errorf("writing %s: %w", destPath, err)
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		os.Remove(tmpPath)
		return 0, fmt.Errorf("renaming temp file to %s: %w", destPath, err)
	}

	return n, nil
}
