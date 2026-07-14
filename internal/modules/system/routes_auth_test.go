package system

import (
	"encoding/base64"
	"testing"
	"time"

	goauth "github.com/MrEthical07/goAuth"
)

func TestBearerToken(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		header string
		want   string
	}{
		{name: "standard bearer", header: "Bearer abc.def.ghi", want: "abc.def.ghi"},
		{name: "case-insensitive scheme", header: "bearer abc", want: "abc"},
		{name: "surrounding spaces trimmed", header: "  Bearer   token  ", want: "token"},
		{name: "empty header", header: "", want: ""},
		{name: "missing scheme", header: "abc.def", want: ""},
		{name: "scheme only", header: "Bearer ", want: ""},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := bearerToken(tc.header); got != tc.want {
				t.Fatalf("bearerToken(%q)=%q want %q", tc.header, got, tc.want)
			}
		})
	}
}

func TestBuildLoginResponseMFAChallenge(t *testing.T) {
	t.Parallel()

	resp, err := buildLoginResponse(loginOutcome{
		MFARequired: true,
		MFAType:     "totp",
		MFASession:  "chal-123",
		MFATypes:    []string{"totp", "webauthn"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.MFARequired {
		t.Fatal("expected MFARequired to be set")
	}
	if resp.MFAChallenge != "chal-123" {
		t.Fatalf("challenge=%q want chal-123", resp.MFAChallenge)
	}
	if resp.MFAType != "totp" {
		t.Fatalf("type=%q want totp", resp.MFAType)
	}
	if resp.AccessToken != "" || resp.RefreshToken != "" {
		t.Fatal("expected no tokens on an MFA challenge response")
	}
	if len(resp.MFATypes) != 2 {
		t.Fatalf("mfa types=%v want 2", resp.MFATypes)
	}
}

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
