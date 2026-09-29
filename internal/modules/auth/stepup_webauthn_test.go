package auth

import (
	"encoding/base64"
	"net/http"
)

func init() {
	credentialID := base64.RawURLEncoding.EncodeToString([]byte("credential-1"))
	// WebAuthn is disabled in the test engine, so getting past the password
	// check ends in goAuth's "disabled" refusal (403).
	extraStepUpEndpoints = append(extraStepUpEndpoints,
		stepUpEndpoint{
			name: "webauthn register begin",
			path: "/api/v1/auth/webauthn/register/begin",
			body: func(p *string) map[string]any { return withPassword(map[string]any{}, p) },
			ok:   func(status int) bool { return status == http.StatusForbidden },
		},
		stepUpEndpoint{
			name: "webauthn credential remove",
			path: "/api/v1/auth/webauthn/credentials/remove",
			body: func(p *string) map[string]any {
				return withPassword(map[string]any{"credential_id": credentialID}, p)
			},
			ok: func(status int) bool { return status == http.StatusForbidden },
		},
	)
}
