package auth

import (
	"context"
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// storedHashFor returns the hash currently persisted for an email.
func storedHashFor(t *testing.T, repo *memoryRepository, email string) string {
	t.Helper()

	user, err := repo.GetUserByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("get user %s: %v", email, err)
	}
	return user.PasswordHash
}

// New registrations must be hashed at the configured cost, not the library
// default of 10.
func TestRegister_HashesAtConfiguredCost(t *testing.T) {
	service, repo := newTestService(t)

	if _, err := service.Register(context.Background(), "cost@example.com", "password123"); err != nil {
		t.Fatalf("register: %v", err)
	}

	hash := storedHashFor(t, repo, "cost@example.com")

	cost, err := bcrypt.Cost([]byte(hash))
	if err != nil {
		t.Fatalf("read cost from hash: %v", err)
	}
	if cost != bcryptCost {
		t.Errorf("hash cost = %d, want %d", cost, bcryptCost)
	}
	if cost == bcrypt.DefaultCost {
		t.Errorf("hash cost = %d, which is bcrypt.DefaultCost; the cost was never raised", cost)
	}
	if cost < 12 {
		t.Errorf("hash cost = %d, want at least 12", cost)
	}
}

// A hash produced at the new cost must still verify through the real login path.
func TestRegisterAndLogin_RoundTripsAtNewCost(t *testing.T) {
	service, _ := newTestService(t)
	ctx := context.Background()

	if _, err := service.Register(ctx, "round@example.com", "password123"); err != nil {
		t.Fatalf("register: %v", err)
	}

	if _, err := service.Login(ctx, "round@example.com", "password123"); err != nil {
		t.Fatalf("login with correct password failed: %v", err)
	}
	if _, err := service.Login(ctx, "round@example.com", "wrong-password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("login with wrong password = %v, want ErrInvalidCredentials", err)
	}
}

// Raising the cost must not lock out users whose hashes were created earlier at
// the old default of 10: CompareHashAndPassword reads the cost from the stored
// hash, not from bcryptCost.
func TestLogin_VerifiesLegacyCost10Hash(t *testing.T) {
	service, repo := newTestService(t)
	ctx := context.Background()

	const legacyPassword = "legacy-password-123"

	// Simulate a user registered before the cost was raised.
	legacyHash, err := bcrypt.GenerateFromPassword([]byte(legacyPassword), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("generate legacy hash: %v", err)
	}

	legacyCost, err := bcrypt.Cost(legacyHash)
	if err != nil {
		t.Fatalf("read legacy cost: %v", err)
	}
	if legacyCost != 10 {
		t.Fatalf("test setup wrong: legacy cost = %d, want 10", legacyCost)
	}
	// Guard the premise: the legacy cost must actually differ from the new one,
	// otherwise this test proves nothing.
	if legacyCost == bcryptCost {
		t.Fatalf("legacy cost equals bcryptCost; the test would be vacuous")
	}

	if err := repo.CreateUser(ctx, "legacy-user-id", "legacy@example.com", string(legacyHash)); err != nil {
		t.Fatalf("seed legacy user: %v", err)
	}

	if _, err := service.Login(ctx, "legacy@example.com", legacyPassword); err != nil {
		t.Fatalf("legacy cost-10 hash must still verify after raising the cost, got: %v", err)
	}

	// And a wrong password must still be rejected for a legacy hash.
	if _, err := service.Login(ctx, "legacy@example.com", "not-the-password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("legacy hash with wrong password = %v, want ErrInvalidCredentials", err)
	}
}

// bcrypt identifies a hash by its embedded cost, so a cost-10 and a cost-12
// hash of the same password both remain independently verifiable.
func TestLogin_VerifiesMixedCostHashes(t *testing.T) {
	service, repo := newTestService(t)
	ctx := context.Background()

	const sharedPassword = "shared-password-123"

	cost10, err := bcrypt.GenerateFromPassword([]byte(sharedPassword), 10)
	if err != nil {
		t.Fatalf("generate cost-10 hash: %v", err)
	}
	cost12, err := bcrypt.GenerateFromPassword([]byte(sharedPassword), bcryptCost)
	if err != nil {
		t.Fatalf("generate cost-%d hash: %v", bcryptCost, err)
	}

	users := []struct {
		id    string
		email string
		hash  []byte
	}{
		{id: "user-10", email: "cost10@example.com", hash: cost10},
		{id: "user-12", email: "cost12@example.com", hash: cost12},
	}

	for _, u := range users {
		if err := repo.CreateUser(ctx, u.id, u.email, string(u.hash)); err != nil {
			t.Fatalf("seed %s: %v", u.email, err)
		}
		if _, err := service.Login(ctx, u.email, sharedPassword); err != nil {
			t.Errorf("login for %s failed: %v", u.email, err)
		}
		if _, err := service.Login(ctx, u.email, "wrong-password"); !errors.Is(err, ErrInvalidCredentials) {
			t.Errorf("wrong password for %s = %v, want ErrInvalidCredentials", u.email, err)
		}
	}
}

// The plaintext must never be recoverable from the stored hash.
func TestRegister_HashDoesNotContainPlaintext(t *testing.T) {
	service, repo := newTestService(t)

	const password = "plaintext-canary-987"

	if _, err := service.Register(context.Background(), "canary@example.com", password); err != nil {
		t.Fatalf("register: %v", err)
	}

	hash := storedHashFor(t, repo, "canary@example.com")
	if strings.Contains(hash, password) {
		t.Errorf("hash contains the plaintext password: %q", hash)
	}
}

// Identical passwords must produce distinct hashes, i.e. the salt is random.
func TestRegister_SaltsAreUnique(t *testing.T) {
	service, repo := newTestService(t)
	ctx := context.Background()

	for _, email := range []string{"a@example.com", "b@example.com"} {
		if _, err := service.Register(ctx, email, "same-password-123"); err != nil {
			t.Fatalf("register %s: %v", email, err)
		}
	}

	first := storedHashFor(t, repo, "a@example.com")
	second := storedHashFor(t, repo, "b@example.com")

	if first == second {
		t.Error("identical passwords produced identical hashes; the salt is not random")
	}
}

func BenchmarkHashPasswordCost10(b *testing.B) {
	password := []byte("benchmark-password-123")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = bcrypt.GenerateFromPassword(password, 10)
	}
}

func BenchmarkHashPasswordCost12(b *testing.B) {
	password := []byte("benchmark-password-123")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = bcrypt.GenerateFromPassword(password, 12)
	}
}

func BenchmarkVerifyPasswordCost10(b *testing.B) {
	password := []byte("benchmark-password-123")
	hash, err := bcrypt.GenerateFromPassword(password, 10)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = bcrypt.CompareHashAndPassword(hash, password)
	}
}

func BenchmarkVerifyPasswordCost12(b *testing.B) {
	password := []byte("benchmark-password-123")
	hash, err := bcrypt.GenerateFromPassword(password, 12)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = bcrypt.CompareHashAndPassword(hash, password)
	}
}
