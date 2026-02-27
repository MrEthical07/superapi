package auth

import (
	"context"
	"errors"
	"fmt"

	goauth "github.com/MrEthical07/goAuth"
	"github.com/redis/go-redis/v9"
)

type providerCloser interface {
	Close()
}

type goAuthValidator interface {
	Validate(ctx context.Context, tokenStr string, routeMode goauth.RouteMode) (*goauth.AuthResult, error)
}

type GoAuthProvider struct {
	validator goAuthValidator
}

func NewGoAuthProvider(validator goAuthValidator) Provider {
	if validator == nil {
		return NewDisabledProvider()
	}
	return &GoAuthProvider{validator: validator}
}

func (p *GoAuthProvider) Authenticate(ctx context.Context, token string, mode Mode) (AuthContext, error) {
	if p == nil || p.validator == nil {
		return AuthContext{}, ErrUnauthenticated
	}

	result, err := p.validator.Validate(ctx, token, toGoAuthRouteMode(mode))
	if err != nil {
		if errors.Is(err, goauth.ErrUnauthorized) || errors.Is(err, goauth.ErrTokenInvalid) || errors.Is(err, goauth.ErrTokenClockSkew) {
			return AuthContext{}, ErrUnauthenticated
		}
		return AuthContext{}, ErrUnauthenticated
	}
	if result == nil {
		return AuthContext{}, ErrUnauthenticated
	}

	principal := AuthContext{
		UserID:      result.UserID,
		TenantID:    result.TenantID,
		Role:        result.Role,
		Permissions: append([]string(nil), result.Permissions...),
	}

	if principal.UserID == "" {
		return AuthContext{}, ErrUnauthenticated
	}

	return principal, nil
}

func NewGoAuthEngineProvider(redisClient redis.UniversalClient, mode Mode) (Provider, func(), error) {
	if redisClient == nil {
		return nil, nil, fmt.Errorf("goAuth provider requires redis client")
	}

	cfg := goauth.DefaultConfig()
	cfg.ValidationMode = toGoAuthValidationMode(mode)
	cfg.Result.IncludeRole = true
	cfg.Result.IncludePermissions = true
	cfg.Account.Enabled = false

	engine, err := goauth.New().
		WithConfig(cfg).
		WithRedis(redisClient).
		WithPermissions([]string{"system.whoami"}).
		WithRoles(map[string][]string{
			"user":  {"system.whoami"},
			"admin": {"system.whoami"},
		}).
		WithUserProvider(noopUserProvider{}).
		Build()
	if err != nil {
		return nil, nil, fmt.Errorf("build goAuth engine: %w", err)
	}

	provider := NewGoAuthProvider(engine)
	shutdown := func() {
		if closer, ok := any(engine).(providerCloser); ok {
			closer.Close()
		}
	}

	return provider, shutdown, nil
}

func toGoAuthRouteMode(mode Mode) goauth.RouteMode {
	switch mode {
	case ModeJWTOnly:
		return goauth.ModeJWTOnly
	case ModeStrict:
		return goauth.ModeStrict
	case ModeHybrid:
		return goauth.ModeHybrid
	default:
		return goauth.ModeInherit
	}
}

func toGoAuthValidationMode(mode Mode) goauth.ValidationMode {
	switch mode {
	case ModeJWTOnly:
		return goauth.ModeJWTOnly
	case ModeStrict:
		return goauth.ModeStrict
	case ModeHybrid:
		return goauth.ModeHybrid
	default:
		return goauth.ModeHybrid
	}
}

type noopUserProvider struct{}

func (noopUserProvider) GetUserByIdentifier(string) (goauth.UserRecord, error) {
	return goauth.UserRecord{}, goauth.ErrUserNotFound
}

func (noopUserProvider) GetUserByID(string) (goauth.UserRecord, error) {
	return goauth.UserRecord{}, goauth.ErrUserNotFound
}

func (noopUserProvider) UpdatePasswordHash(string, string) error {
	return goauth.ErrUnauthorized
}

func (noopUserProvider) CreateUser(context.Context, goauth.CreateUserInput) (goauth.UserRecord, error) {
	return goauth.UserRecord{}, goauth.ErrUnauthorized
}

func (noopUserProvider) UpdateAccountStatus(context.Context, string, goauth.AccountStatus) (goauth.UserRecord, error) {
	return goauth.UserRecord{}, goauth.ErrUnauthorized
}

func (noopUserProvider) GetTOTPSecret(context.Context, string) (*goauth.TOTPRecord, error) {
	return nil, goauth.ErrUnauthorized
}

func (noopUserProvider) EnableTOTP(context.Context, string, []byte) error {
	return goauth.ErrUnauthorized
}

func (noopUserProvider) DisableTOTP(context.Context, string) error {
	return goauth.ErrUnauthorized
}

func (noopUserProvider) MarkTOTPVerified(context.Context, string) error {
	return goauth.ErrUnauthorized
}

func (noopUserProvider) UpdateTOTPLastUsedCounter(context.Context, string, int64) error {
	return goauth.ErrUnauthorized
}

func (noopUserProvider) GetBackupCodes(context.Context, string) ([]goauth.BackupCodeRecord, error) {
	return nil, goauth.ErrUnauthorized
}

func (noopUserProvider) ReplaceBackupCodes(context.Context, string, []goauth.BackupCodeRecord) error {
	return goauth.ErrUnauthorized
}

func (noopUserProvider) ConsumeBackupCode(context.Context, string, [32]byte) (bool, error) {
	return false, goauth.ErrUnauthorized
}
