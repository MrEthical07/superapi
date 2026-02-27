package auth

import (
	"context"
	"errors"
	"testing"

	goauth "github.com/MrEthical07/goAuth"
)

type fakeValidator struct {
	result *goauth.AuthResult
	err    error
	mode   goauth.RouteMode
}

func (f *fakeValidator) Validate(ctx context.Context, tokenStr string, routeMode goauth.RouteMode) (*goauth.AuthResult, error) {
	f.mode = routeMode
	return f.result, f.err
}

func TestGoAuthProviderAuthenticateSuccess(t *testing.T) {
	v := &fakeValidator{result: &goauth.AuthResult{UserID: "u1", TenantID: "t1", Role: "admin", Permissions: []string{"system.whoami"}}}
	p := NewGoAuthProvider(v)

	principal, err := p.Authenticate(context.Background(), "token", ModeStrict)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if principal.UserID != "u1" {
		t.Fatalf("principal.UserID=%q want=%q", principal.UserID, "u1")
	}
	if v.mode != goauth.ModeStrict {
		t.Fatalf("route mode=%v want=%v", v.mode, goauth.ModeStrict)
	}
}

func TestGoAuthProviderAuthenticateUnauthorized(t *testing.T) {
	v := &fakeValidator{err: goauth.ErrUnauthorized}
	p := NewGoAuthProvider(v)

	_, err := p.Authenticate(context.Background(), "token", ModeHybrid)
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("expected ErrUnauthenticated, got %v", err)
	}
}

func TestParseMode(t *testing.T) {
	cases := []struct {
		in   string
		want Mode
	}{
		{in: "jwt_only", want: ModeJWTOnly},
		{in: "jwt-only", want: ModeJWTOnly},
		{in: "hybrid", want: ModeHybrid},
		{in: "strict", want: ModeStrict},
	}

	for _, tc := range cases {
		got, err := ParseMode(tc.in)
		if err != nil {
			t.Fatalf("ParseMode(%q) error = %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("ParseMode(%q)=%q want=%q", tc.in, got, tc.want)
		}
	}

	if _, err := ParseMode("invalid"); err == nil {
		t.Fatalf("expected error for invalid mode")
	}
}
