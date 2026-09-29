package main

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/db/dbtest"
	"github.com/MrEthical07/superapi/internal/core/storage"
)

func key(b byte) []byte { return bytes.Repeat([]byte{b}, 32) }

func ring(t *testing.T, active string, keys map[string][]byte, legacy []byte) auth.RotatableCipher {
	t.Helper()
	c, err := auth.NewKeyRingCipher(auth.KeyRing{Keys: keys, ActiveKID: active, LegacyKey: legacy})
	if err != nil {
		t.Fatalf("keyring: %v", err)
	}
	return c
}

// sealV1 reproduces the v0.9.0 ciphertext format.
func sealV1(t *testing.T, k []byte, userID string, plaintext []byte) []byte {
	t.Helper()
	block, _ := aes.NewCipher(k)
	aead, _ := cipher.NewGCM(block)
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	return aead.Seal(append([]byte{1}, nonce...), nonce, plaintext, []byte("superapi/totp/v1:"+userID))
}

type fixture struct {
	pg    *storage.Postgres
	mfa   auth.MFARepository
	users auth.UserRepository
	ids   []string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	pg := dbtest.NewPostgres(t)
	return &fixture{pg: pg, mfa: auth.NewMFARepository(pg), users: auth.NewRelationalUserRepository(pg)}
}

// addUser creates a user with a stored TOTP secret (ciphertext built from the
// user's id, which is only known after creation).
func (f *fixture) addUser(t *testing.T, name string, seal func(userID string) []byte) string {
	t.Helper()
	ctx := context.Background()
	u, err := f.users.Create(ctx, auth.CreateStoredUserInput{Identifier: name + "@example.com", PasswordHash: "h", Status: "active"})
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	if err := f.mfa.UpsertTOTPSecret(ctx, "", u.ID, seal(u.ID)); err != nil {
		t.Fatalf("store secret for %s: %v", name, err)
	}
	f.ids = append(f.ids, u.ID)
	return u.ID
}

func (f *fixture) ciphertext(t *testing.T, userID string) []byte {
	t.Helper()
	state, found, err := f.mfa.GetTOTP(context.Background(), "", userID)
	if err != nil || !found {
		t.Fatalf("GetTOTP: found=%v err=%v", found, err)
	}
	return state.SecretCiphertext
}

func TestRotateReEncryptsEverythingInBatches(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	oldRing := ring(t, "old", map[string][]byte{"old": key(1)}, nil)
	newRing := ring(t, "new", map[string][]byte{"old": key(1), "new": key(2)}, key(7))

	plaintexts := map[string]string{}
	add := func(name string, seal func(id string) ([]byte, string)) string {
		var secret string
		id := f.addUser(t, name, func(id string) []byte {
			ct, s := seal(id)
			secret = s
			return ct
		})
		plaintexts[id] = secret
		return id
	}
	v1 := []string{
		add("v1-a", func(id string) ([]byte, string) { return sealV1(t, key(7), id, []byte("seed-v1-a")), "seed-v1-a" }),
		add("v1-b", func(id string) ([]byte, string) { return sealV1(t, key(7), id, []byte("seed-v1-b")), "seed-v1-b" }),
	}
	oldKid := []string{
		add("old-a", func(id string) ([]byte, string) {
			ct, _ := oldRing.Seal(id, []byte("seed-old-a"))
			return ct, "seed-old-a"
		}),
		add("old-b", func(id string) ([]byte, string) {
			ct, _ := oldRing.Seal(id, []byte("seed-old-b"))
			return ct, "seed-old-b"
		}),
		add("old-c", func(id string) ([]byte, string) {
			ct, _ := oldRing.Seal(id, []byte("seed-old-c"))
			return ct, "seed-old-c"
		}),
	}
	_ = []string{
		add("cur-a", func(id string) ([]byte, string) {
			ct, _ := newRing.Seal(id, []byte("seed-cur-a"))
			return ct, "seed-cur-a"
		}),
		add("cur-b", func(id string) ([]byte, string) {
			ct, _ := newRing.Seal(id, []byte("seed-cur-b"))
			return ct, "seed-cur-b"
		}),
	}

	var out bytes.Buffer
	res, err := rotate(ctx, f.mfa, newRing, options{batchSize: 2}, &out)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if res.Scanned != 7 || res.Rotated != 5 || res.Current != 2 || res.Skipped != 0 || res.Failed != 0 {
		t.Fatalf("result = %+v, want scanned 7, rotated 5, current 2", res)
	}

	// Every row now opens with only the new key, and the plaintext is intact.
	newOnly := ring(t, "new", map[string][]byte{"new": key(2)}, nil)
	for id, want := range plaintexts {
		got, err := newOnly.Open(id, f.ciphertext(t, id))
		if err != nil || string(got) != want {
			t.Fatalf("user %s after rotation: %q %v, want %q", id, got, err, want)
		}
	}
	for _, id := range append(append([]string{}, v1...), oldKid...) {
		if newRing.NeedsRotation(f.ciphertext(t, id)) {
			t.Fatalf("user %s was not rotated", id)
		}
	}

	// Progress: 7 rows in batches of 2 = 4 batches, each reported.
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 4 || !strings.HasPrefix(lines[0], "batch 1: scanned=2") || !strings.HasPrefix(lines[3], "batch 4: scanned=7") {
		t.Fatalf("progress output:\n%s", out.String())
	}
	for _, line := range lines {
		for _, secret := range plaintexts {
			if strings.Contains(line, secret) {
				t.Fatalf("progress output leaks a secret: %s", line)
			}
		}
	}

	// A second run finds nothing to do.
	res, err = rotate(ctx, f.mfa, newRing, options{batchSize: 3}, &bytes.Buffer{})
	if err != nil || res.Scanned != 7 || res.Rotated != 0 || res.Current != 7 || res.Failed != 0 {
		t.Fatalf("second run = %+v err=%v, want all 7 current", res, err)
	}
}

func TestRotateDryRunWritesNothing(t *testing.T) {
	f := newFixture(t)
	oldRing := ring(t, "old", map[string][]byte{"old": key(1)}, nil)
	id := f.addUser(t, "dry", func(id string) []byte {
		ct, _ := oldRing.Seal(id, []byte("seed"))
		return ct
	})
	before := append([]byte(nil), f.ciphertext(t, id)...)

	newRing := ring(t, "new", map[string][]byte{"old": key(1), "new": key(2)}, nil)
	res, err := rotate(context.Background(), f.mfa, newRing, options{batchSize: 10, dryRun: true}, &bytes.Buffer{})
	if err != nil || res.Scanned != 1 || res.Rotated != 1 || res.Failed != 0 {
		t.Fatalf("dry run = %+v err=%v", res, err)
	}
	if !bytes.Equal(before, f.ciphertext(t, id)) {
		t.Fatal("a dry run changed a row")
	}
	var buf bytes.Buffer
	res.print(&buf, true)
	if !strings.Contains(buf.String(), "would be re-encrypted") {
		t.Fatalf("dry-run summary: %s", buf.String())
	}
}

// Only rows sealed under a key that is no longer configured fail; they are
// reported with the missing key's id and left untouched, and every other row is
// still rotated.
func TestRotateReportsRowsSealedWithARemovedKey(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	gone := ring(t, "gone", map[string][]byte{"gone": key(5)}, nil)
	older := ring(t, "old", map[string][]byte{"old": key(1)}, nil)

	brokenID := f.addUser(t, "broken", func(id string) []byte { ct, _ := gone.Seal(id, []byte("lost")); return ct })
	okID := f.addUser(t, "fine", func(id string) []byte { ct, _ := older.Seal(id, []byte("kept")); return ct })
	brokenBefore := append([]byte(nil), f.ciphertext(t, brokenID)...)

	newRing := ring(t, "new", map[string][]byte{"old": key(1), "new": key(2)}, nil)
	res, err := rotate(ctx, f.mfa, newRing, options{batchSize: 10}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if res.Scanned != 2 || res.Rotated != 1 || res.Failed != 1 || len(res.Failures) != 1 {
		t.Fatalf("result = %+v", res)
	}
	if res.Failures[0].UserID != brokenID || !strings.Contains(res.Failures[0].Reason, `"gone"`) {
		t.Fatalf("failure = %+v, want the user and the missing key id", res.Failures[0])
	}
	if !bytes.Equal(brokenBefore, f.ciphertext(t, brokenID)) {
		t.Fatal("an undecryptable row must be left unchanged")
	}
	if newRing.NeedsRotation(f.ciphertext(t, okID)) {
		t.Fatal("a good row must still be rotated when another row fails")
	}

	var buf bytes.Buffer
	res.print(&buf, false)
	if !strings.Contains(buf.String(), "failed user "+brokenID) {
		t.Fatalf("summary should list the failing user: %s", buf.String())
	}
}

// A secret changed by someone else between the read and the write is not
// overwritten: the swap is compare-and-swap and the row counts as skipped.
type racingRepo struct {
	auth.MFARepository
}

func (r *racingRepo) RotateTOTPSecret(ctx context.Context, tenantID, userID string, prev, next []byte) (bool, error) {
	// A user re-enrolls (or a login rotates the row) right before our write.
	if err := r.UpsertTOTPSecret(ctx, tenantID, userID, []byte("changed-under-us")); err != nil {
		return false, err
	}
	return r.MFARepository.RotateTOTPSecret(ctx, tenantID, userID, prev, next)
}

func TestRotateDoesNotOverwriteAConcurrentChange(t *testing.T) {
	f := newFixture(t)
	oldRing := ring(t, "old", map[string][]byte{"old": key(1)}, nil)
	id := f.addUser(t, "race", func(id string) []byte { ct, _ := oldRing.Seal(id, []byte("seed")); return ct })

	newRing := ring(t, "new", map[string][]byte{"old": key(1), "new": key(2)}, nil)
	repo := &racingRepo{MFARepository: f.mfa}
	res, err := rotate(context.Background(), repo, newRing, options{batchSize: 10}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if res.Skipped != 1 || res.Rotated != 0 || res.Failed != 0 {
		t.Fatalf("result = %+v, want the row skipped", res)
	}
	if string(f.ciphertext(t, id)) != "changed-under-us" {
		t.Fatal("the concurrent change was overwritten by a stale re-encryption")
	}
}

type failingList struct{ auth.MFARepository }

func (failingList) ListTOTPSecrets(context.Context, string, int) ([]auth.TOTPSecretRow, error) {
	return nil, errors.New("connection reset")
}

func TestRotateStopsOnAReadError(t *testing.T) {
	newRing := ring(t, "new", map[string][]byte{"new": key(2)}, nil)
	_, err := rotate(context.Background(), failingList{}, newRing, options{batchSize: 10}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "read batch 1") {
		t.Fatalf("err = %v, want a read-batch error", err)
	}
}

func TestRotateHonoursCancellation(t *testing.T) {
	f := newFixture(t)
	oldRing := ring(t, "old", map[string][]byte{"old": key(1)}, nil)
	for i := 0; i < 3; i++ {
		f.addUser(t, fmt.Sprintf("c%d", i), func(id string) []byte { ct, _ := oldRing.Seal(id, []byte("s")); return ct })
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	newRing := ring(t, "new", map[string][]byte{"old": key(1), "new": key(2)}, nil)
	if _, err := rotate(ctx, f.mfa, newRing, options{batchSize: 1}, &bytes.Buffer{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestRotateWithNoSecretsIsANoOp(t *testing.T) {
	f := newFixture(t)
	newRing := ring(t, "new", map[string][]byte{"new": key(2)}, nil)
	res, err := rotate(context.Background(), f.mfa, newRing, options{batchSize: 10}, &bytes.Buffer{})
	if err != nil || res.Scanned != 0 {
		t.Fatalf("result = %+v err=%v", res, err)
	}
}

func TestParseFlags(t *testing.T) {
	tests := []struct {
		args    []string
		want    options
		wantErr string
	}{
		{nil, options{batchSize: 200}, ""},
		{[]string{"--dry-run", "--batch-size", "50"}, options{batchSize: 50, dryRun: true}, ""},
		{[]string{"--batch-size", "0"}, options{}, "between 1 and 10000"},
		{[]string{"--batch-size", "10001"}, options{}, "between 1 and 10000"},
		{[]string{"extra"}, options{}, "unexpected arguments"},
		{[]string{"--nope"}, options{}, "flag provided but not defined"},
	}
	for _, tc := range tests {
		got, err := parseFlags(tc.args, &bytes.Buffer{})
		if tc.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("args %v: err = %v, want containing %q", tc.args, err, tc.wantErr)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Fatalf("args %v: got %+v err=%v, want %+v", tc.args, got, err, tc.want)
		}
	}
}

func TestResultPrintBoundsTheFailureList(t *testing.T) {
	var res result
	for i := 0; i < maxReportedFailures+5; i++ {
		res.fail(fmt.Sprintf("user-%d", i), errors.New("boom"))
	}
	var buf bytes.Buffer
	res.print(&buf, false)
	out := buf.String()
	if strings.Count(out, "failed user") != maxReportedFailures || !strings.Contains(out, "and 5 more") {
		t.Fatalf("output:\n%s", out)
	}
}
