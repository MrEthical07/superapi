package auth

import "testing"

func TestNormalizeIdentifier(t *testing.T) {
	tests := []struct{ in, want string }{
		{"alice@example.com", "alice@example.com"},
		{"Alice@Example.com", "alice@example.com"},
		{"ALICE@EXAMPLE.COM", "alice@example.com"},
		{"  Alice@Example.com\t\n", "alice@example.com"},
		{"", ""},
		{"   ", ""},
		{"ÄLICE@Example.com", "älice@example.com"},
	}
	for _, tc := range tests {
		if got := NormalizeIdentifier(tc.in); got != tc.want {
			t.Errorf("NormalizeIdentifier(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeIdentifierIsIdempotent(t *testing.T) {
	for _, in := range []string{"Alice@Example.com", " BOB@x.io ", "ǅ@example.com"} {
		once := NormalizeIdentifier(in)
		if twice := NormalizeIdentifier(once); twice != once {
			t.Errorf("not idempotent for %q: %q then %q", in, once, twice)
		}
	}
}
