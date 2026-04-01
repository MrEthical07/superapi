package auth

import (
	"errors"
	"strings"
)

var (
	ErrUnauthenticated = errors.New("unauthenticated")
	ErrForbidden       = errors.New("forbidden")
)

type Mode string

const (
	ModeJWTOnly Mode = "jwt_only"
	ModeHybrid  Mode = "hybrid"
	ModeStrict  Mode = "strict"
)

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
