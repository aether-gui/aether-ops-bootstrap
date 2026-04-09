package launcher

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"strings"
	"syscall"
)

// Preflight checks that the host meets the minimum requirements for bootstrap.
func Preflight() error {
	if os.Getuid() != 0 {
		return fmt.Errorf("must run as root")
	}

	if runtime.GOARCH != "amd64" {
		return fmt.Errorf("unsupported architecture %s (only amd64 is supported)", runtime.GOARCH)
	}

	if _, err := os.Stat("/run/systemd/system"); os.IsNotExist(err) {
		return fmt.Errorf("systemd not detected (/run/systemd/system missing)")
	}

	suite, err := detectSuite()
	if err != nil {
		return fmt.Errorf("detecting Ubuntu suite: %w", err)
	}
	switch suite {
	case "jammy", "noble", "plucky":
		// supported
	default:
		return fmt.Errorf("unsupported Ubuntu suite %q (supported: jammy, noble, plucky)", suite)
	}

	if err := checkDiskSpace("/", 10*1024*1024*1024); err != nil {
		return err
	}

	return nil
}

// DetectSuite reads the Ubuntu codename from /etc/os-release.
func DetectSuite() (string, error) {
	return detectSuite()
}

func detectSuite() (string, error) {
	return parseOSRelease("/etc/os-release", "VERSION_CODENAME")
}

func parseOSRelease(path, key string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, key+"=") {
			val := strings.TrimPrefix(line, key+"=")
			val = strings.Trim(val, "\"")
			return val, nil
		}
	}

	return "", fmt.Errorf("%s not found in %s", key, path)
}

func checkDiskSpace(path string, minBytes uint64) error {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return fmt.Errorf("checking disk space on %s: %w", path, err)
	}
	available := stat.Bavail * uint64(stat.Bsize)
	if available < minBytes {
		return fmt.Errorf("insufficient disk space on %s: %d MB available, need at least %d MB",
			path, available/1024/1024, minBytes/1024/1024)
	}
	return nil
}
