package auth

import (
	"errors"
	"strings"
)

var (
	// ErrUnauthenticated signals missing or invalid authentication context.
	ErrUnauthenticated = errors.New("unauthenticated")
	// ErrForbidden signals authenticated access without sufficient authorization.
	ErrForbidden = errors.New("forbidden")
)

// Mode selects auth validation strictness.
type Mode string

const (
	// ModeJWTOnly validates only JWT claims and signature.
	ModeJWTOnly Mode = "jwt_only"
	// ModeHybrid prefers strict checks but can fallback when dependencies fail.
	ModeHybrid Mode = "hybrid"
	// ModeStrict requires backing session checks for revocation-aware auth.
	ModeStrict Mode = "strict"
)

// ParseMode normalizes mode input into a supported auth Mode value.
//
// Empty values default to ModeHybrid to keep startup behavior predictable.
func ParseMode(mode string) (Mode, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "jwt_only", "jwt-only", "jwtonly":
		return ModeJWTOnly, nil
	case "hybrid", "":
		return ModeHybrid, nil
	case "strict":
		return ModeStrict, nil
	default:
		return "", errors.New("invalid auth mode")
	}
}
