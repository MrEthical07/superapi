package auth

import (
	"encoding/base64"
	"testing"
	"time"

	goauth "github.com/MrEthical07/goAuth"
)

func TestWebAuthnCredentialToView(t *testing.T) {
	t.Parallel()

	rawID := []byte{0x01, 0x02, 0xff, 0xfe}
	created := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	view := webAuthnCredentialToView(goauth.WebAuthnCredential{
		CredentialID: rawID,
		SignCount:    7,
		CreatedAt:    created,
	})

	// CredentialID must be base64url-encoded so it round-trips through the
	// remove endpoint's decoder.
	decoded, err := base64.RawURLEncoding.DecodeString(view.CredentialID)
	if err != nil {
		t.Fatalf("credential id is not base64url: %v", err)
	}
	if string(decoded) != string(rawID) {
		t.Fatalf("credential id round-trip mismatch: got %v want %v", decoded, rawID)
	}
	if view.SignCount != 7 {
		t.Fatalf("sign count=%d want 7", view.SignCount)
	}
	if view.CreatedUTC != "2026-07-14T10:00:00Z" {
		t.Fatalf("created=%q want 2026-07-14T10:00:00Z", view.CreatedUTC)
	}
	if view.LastUsedUTC != "" {
		t.Fatalf("expected empty last-used for zero time, got %q", view.LastUsedUTC)
	}
}
