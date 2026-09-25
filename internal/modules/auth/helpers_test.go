package auth

import (
	"testing"
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
