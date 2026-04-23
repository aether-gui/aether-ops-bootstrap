package launcher

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log"
	"os"

	"github.com/aether-gui/aether-ops-bootstrap/internal/installctx"
)

// OnrampPasswordEnvVar is the environment variable consulted for the
// onramp user's password when neither the CLI flag nor the spec sets it.
const OnrampPasswordEnvVar = "AETHER_ONRAMP_PASSWORD"

// ResolveOnrampPassword picks the password the onramp user should be
// created with, in this precedence:
//
//  1. the --onramp-password CLI flag (cliFlag argument)
//  2. the AETHER_ONRAMP_PASSWORD environment variable
//  3. the spec / manifest field (manifestPassword argument)
//  4. a newly generated random password, which is also written to stderr
//     with a visible banner so unattended runs do not silently lose it
//
// The returned source is one of "flag", "env", "spec", or "generated" and
// is intended for logging only. The returned password is always
// validated via installctx.ValidateOnrampPassword so downstream consumers (chpasswd,
// hosts.ini) never see NUL or line-delimiter characters that could
// corrupt their on-disk format.
func ResolveOnrampPassword(cliFlag, manifestPassword string) (string, string, error) {
	if cliFlag != "" {
		if err := installctx.ValidateOnrampPassword(cliFlag); err != nil {
			return "", "", fmt.Errorf("--onramp-password: %w", err)
		}
		return cliFlag, "flag", nil
	}
	if v := os.Getenv(OnrampPasswordEnvVar); v != "" {
		if err := installctx.ValidateOnrampPassword(v); err != nil {
			return "", "", fmt.Errorf("%s: %w", OnrampPasswordEnvVar, err)
		}
		return v, "env", nil
	}
	if manifestPassword != "" {
		if err := installctx.ValidateOnrampPassword(manifestPassword); err != nil {
			return "", "", fmt.Errorf("aether_ops.onramp_password in spec: %w", err)
		}
		return manifestPassword, "spec", nil
	}
	generated, err := generateRandomPassword(24)
	if err != nil {
		return "", "", fmt.Errorf("generating random onramp password: %w", err)
	}
	return generated, "generated", nil
}

// LogGeneratedPassword prints a loud banner to stderr recording a
// password that was generated because no source supplied one. Callers
// should only invoke this when source == "generated"; otherwise the
// password would be written to the log twice over.
//
// Rationale: unattended installs (cloud-init, CI, orchestration) have
// no human at a TTY to notice a random password. Writing it to stderr
// plus the persistent bootstrap log gives the operator a single place
// to retrieve it after the fact.
func LogGeneratedPassword(username, password string) {
	log.Println("")
	log.Println("========================================")
	log.Println("  IMPORTANT: record this password")
	log.Println("========================================")
	log.Printf("  onramp user: %s", username)
	log.Printf("  onramp password (generated): %s", password)
	log.Println("")
	log.Println("  No --onramp-password flag, AETHER_ONRAMP_PASSWORD env var,")
	log.Println("  or aether_ops.onramp_password spec field was provided, so")
	log.Println("  the bootstrap generated a random password. It will not be")
	log.Println("  displayed again.")
	log.Println("========================================")
	log.Println("")
}

// generateRandomPassword returns a raw (unpadded) URL-safe base64
// random string of exactly n characters. The function draws enough
// random bytes to yield at least n encoded characters and then
// truncates. Every output character is drawn from [A-Za-z0-9_-], which
// is safe to splice into chpasswd stdin and ansible hosts.ini without
// quoting.
func generateRandomPassword(n int) (string, error) {
	// 3 raw bytes encode to 4 base64 characters; request enough raw
	// bytes to yield at least n characters.
	raw := make([]byte, (n*3/4)+1)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	if len(encoded) < n {
		return encoded, nil
	}
	return encoded[:n], nil
}
