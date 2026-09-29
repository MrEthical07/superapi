package auth

import (
	"strings"
	"testing"

	"github.com/MrEthical07/superapi/internal/core/config"
)

// TestRPIDLintMatchesGoAuth keeps config.ValidateRPID (which runs at config
// load) in step with goAuth's own decision at Build(): the two must accept and
// reject the same relying party IDs.
func TestRPIDLintMatchesGoAuth(t *testing.T) {
	t.Setenv("AUTH_TEST_SHARED_SECRET", "")
	ids := []string{
		"localhost", "example.com", "auth.example.com", "a-b.example.co.uk", "EXAMPLE.com",
		"127.0.0.1", "::1", "[::1]", "10.0.0.5", "192.168.1.1",
		"", " ", "myhost", "example.123", "123", "example..com", ".example.com", "example.com.",
		"-example.com", "example-.com", "https://example.com", "example.com:8080", "example.com/path",
		"example.com?x=1", "example.com#frag", "user@example.com", "exämple.com", "exa mple.com",
		strings.Repeat("a", 64) + ".com", strings.Repeat("a.", 130) + "com",
	}
	for _, id := range ids {
		t.Run(id, func(t *testing.T) {
			cfg, err := ProjectGoAuthConfig(ModeStrict, Features{})
			if err != nil {
				t.Fatalf("ProjectGoAuthConfig: %v", err)
			}
			cfg.WebAuthn.Enabled = true
			cfg.WebAuthn.RPID = id
			cfg.WebAuthn.RPDisplayName = "SuperAPI"
			cfg.WebAuthn.RPOrigins = []string{"https://example.com"}

			goauthRejects := cfg.Validate() != nil
			lintRejects := config.ValidateRPID(id) != nil
			if goauthRejects != lintRejects {
				t.Fatalf("RPID %q: goAuth rejects=%v, config.ValidateRPID rejects=%v", id, goauthRejects, lintRejects)
			}
		})
	}
}
