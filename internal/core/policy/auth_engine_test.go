package policy

import (
	"context"
	"sync"
	"testing"

	goauth "github.com/MrEthical07/goAuth"
	goauthpassword "github.com/MrEthical07/goAuth/password"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/MrEthical07/superapi/internal/core/auth"
)

const policyTestPassword = "password12345"

type policyTestUserProvider struct {
	mu   sync.RWMutex
	user goauth.UserRecord
}

func newPolicyTestAuthEngine(t testing.TB) (*goauth.Engine, string) {
	t.Helper()

	cfg := goauth.DefaultConfig()
	hasher, err := goauthpassword.NewArgon2(goauthpassword.Config{
		Memory:      cfg.Password.Memory,
		Time:        cfg.Password.Time,
		Parallelism: cfg.Password.Parallelism,
		SaltLength:  cfg.Password.SaltLength,
		KeyLength:   cfg.Password.KeyLength,
	})
	if err != nil {
		t.Fatalf("new argon2 hasher: %v", err)
	}

	hash, err := hasher.Hash(policyTestPassword)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	provider := &policyTestUserProvider{
		user: goauth.UserRecord{
			UserID:            "u1",
			Identifier:        "user@example.com",
			TenantID:          "t1",
			PasswordHash:      hash,
			Status:            goauth.AccountActive,
			Role:              "user",
			PermissionVersion: 1,
			RoleVersion:       1,
			AccountVersion:    1,
		},
	}

	mr := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() {
		_ = redisClient.Close()
		mr.Close()
	})

	engine, closeFn, err := auth.NewGoAuthEngine(redisClient, auth.ModeHybrid, provider)
	if err != nil {
		t.Fatalf("new auth engine: %v", err)
	}
	t.Cleanup(closeFn)

	accessToken, _, err := engine.Login(context.Background(), provider.user.Identifier, policyTestPassword)
	if err != nil {
		t.Fatalf("login for test token: %v", err)
	}

	if _, err := engine.Validate(context.Background(), accessToken, goauth.ModeInherit); err != nil {
		t.Fatalf("validate issued token: %v", err)
	}

	return engine, accessToken
}

func (p *policyTestUserProvider) GetUserByIdentifier(identifier string) (goauth.UserRecord, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if identifier != p.user.Identifier {
		return goauth.UserRecord{}, goauth.ErrUserNotFound
	}
	return p.user, nil
}

func (p *policyTestUserProvider) GetUserByID(userID string) (goauth.UserRecord, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if userID != p.user.UserID {
		return goauth.UserRecord{}, goauth.ErrUserNotFound
	}
	return p.user, nil
}

func (p *policyTestUserProvider) UpdatePasswordHash(userID, newHash string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if userID != p.user.UserID {
		return goauth.ErrUserNotFound
	}
	p.user.PasswordHash = newHash
	return nil
}

func (p *policyTestUserProvider) CreateUser(context.Context, goauth.CreateUserInput) (goauth.UserRecord, error) {
	return goauth.UserRecord{}, goauth.ErrUnauthorized
}

func (p *policyTestUserProvider) UpdateAccountStatus(_ context.Context, userID string, status goauth.AccountStatus) (goauth.UserRecord, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if userID != p.user.UserID {
		return goauth.UserRecord{}, goauth.ErrUserNotFound
	}
	p.user.Status = status
	return p.user, nil
}

func (p *policyTestUserProvider) GetTOTPSecret(context.Context, string) (*goauth.TOTPRecord, error) {
	return nil, goauth.ErrUnauthorized
}

func (p *policyTestUserProvider) EnableTOTP(context.Context, string, []byte) error {
	return goauth.ErrUnauthorized
}

func (p *policyTestUserProvider) DisableTOTP(context.Context, string) error {
	return goauth.ErrUnauthorized
}

func (p *policyTestUserProvider) MarkTOTPVerified(context.Context, string) error {
	return goauth.ErrUnauthorized
}

func (p *policyTestUserProvider) UpdateTOTPLastUsedCounter(context.Context, string, int64) error {
	return goauth.ErrUnauthorized
}

func (p *policyTestUserProvider) GetBackupCodes(context.Context, string) ([]goauth.BackupCodeRecord, error) {
	return nil, goauth.ErrUnauthorized
}

func (p *policyTestUserProvider) ReplaceBackupCodes(context.Context, string, []goauth.BackupCodeRecord) error {
	return goauth.ErrUnauthorized
}

func (p *policyTestUserProvider) ConsumeBackupCode(context.Context, string, [32]byte) (bool, error) {
	return false, goauth.ErrUnauthorized
}
