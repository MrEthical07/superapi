package auth

import (
	"testing"
)

func TestNewGoAuthEngineRequiresRedis(t *testing.T) {
	engine, shutdown, err := NewGoAuthEngine(nil, ModeHybrid, nil)
	if err == nil {
		t.Fatalf("expected error when redis client is nil")
	}
	if engine != nil {
		t.Fatalf("engine=%v want nil", engine)
	}
	if shutdown != nil {
		t.Fatalf("shutdown should be nil when engine creation fails")
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
