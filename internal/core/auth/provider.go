package auth

import (
	"context"
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

type Provider interface {
	Authenticate(ctx context.Context, token string, mode Mode) (AuthContext, error)
}

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

type DisabledProvider struct{}

func NewDisabledProvider() Provider {
	return DisabledProvider{}
}

func (DisabledProvider) Authenticate(context.Context, string, Mode) (AuthContext, error) {
	return AuthContext{}, ErrUnauthenticated
}
