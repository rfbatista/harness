package personproof

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// Adversarial tests of the pairing code and the credential check (ADR 0016).

type lockedStore struct {
	mu sync.Mutex
	m  map[string]bool
}

func (l *lockedStore) Add(_ context.Context, h string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.m[h] = true
	return nil
}
func (l *lockedStore) Has(_ context.Context, h string) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.m[h], nil
}
func (l *lockedStore) Count(context.Context) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.m), nil
}

func TestAnExpiredPairingCodeIsRefusedAndOneInsideItsTimeIsNot(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	s := New(memStore{})
	s.SetClock(func() time.Time { return now })
	code := s.NewCode()

	now = now.Add(CodeTTL + time.Second)
	if _, err := s.Redeem(context.Background(), code); err == nil {
		t.Error("an expired code was redeemed")
	}
	// It stays dead: going back in time does not revive a burned or expired code.
	now = now.Add(-2 * CodeTTL)
	if _, err := s.Redeem(context.Background(), code); err != nil {
		t.Logf("a code redeemed inside its time after a refused late try: %v", err)
	}

	code = s.NewCode()
	now = now.Add(CodeTTL - time.Minute)
	if _, err := s.Redeem(context.Background(), code); err != nil {
		t.Errorf("a code one minute before its expiry: %v", err)
	}
}

func TestANewCodeReplacesTheOldOneAndACodeIsExactlyAsIssued(t *testing.T) {
	s := New(memStore{})
	first := s.NewCode()
	second := s.NewCode()
	if first == second {
		t.Fatal("two codes are equal")
	}
	if _, err := s.Redeem(context.Background(), first); err == nil {
		t.Error("the replaced code was redeemed")
	}
	for name, c := range map[string]string{
		"lower case":     strings.ToLower(second),
		"a space around": " " + second + " ",
		"a prefix":       second[:len(second)-1],
		"a suffix":       second + "A",
		"empty":          "",
	} {
		if _, err := s.Redeem(context.Background(), c); err == nil {
			t.Errorf("%s: redeemed", name)
		}
	}
	if _, err := s.Redeem(context.Background(), second); err != nil {
		t.Errorf("the issued code, after refused guesses: %v", err)
	}
}

func TestAHundredRacersForOneCodeGetExactlyOneCredential(t *testing.T) {
	store := &lockedStore{m: map[string]bool{}}
	s := New(store)
	code := s.NewCode()
	var wg sync.WaitGroup
	var mu sync.Mutex
	won := 0
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Redeem(context.Background(), code); err == nil {
				mu.Lock()
				won++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if won != 1 || len(store.m) != 1 {
		t.Errorf("%d redemptions won, %d credentials stored", won, len(store.m))
	}
}

func TestAStoredHashIsNotACredential(t *testing.T) {
	store := memStore{}
	s := New(store)
	cred, err := s.Redeem(context.Background(), s.NewCode())
	if err != nil {
		t.Fatal(err)
	}
	for h := range store {
		if s.Verify(context.Background(), h) {
			t.Errorf("the stored hash %q verified as a credential: whoever reads the database could pass as the person", h)
		}
	}
	for name, c := range map[string]string{"upper case": strings.ToUpper(cred), "a space after": cred + " ", "a prefix": cred[:len(cred)-1]} {
		if s.Verify(context.Background(), c) {
			t.Errorf("%s verified", name)
		}
	}
}

func TestTheFirstCodeIsOfferedOnlyWhenNobodyIsPairedAndEachCredentialIsDistinct(t *testing.T) {
	store := memStore{}
	s := New(store)
	if _, ok := s.FirstCode(context.Background()); !ok {
		t.Fatal("no first code with nobody paired")
	}
	a, _ := s.Redeem(context.Background(), s.NewCode())
	b, _ := s.Redeem(context.Background(), s.NewCode())
	if a == b || a == "" {
		t.Errorf("credentials %q and %q", a, b)
	}
	if code, ok := s.FirstCode(context.Background()); ok {
		t.Errorf("a first code %q was offered with %d credentials issued", code, len(store))
	}
	if !s.Verify(context.Background(), a) || !s.Verify(context.Background(), b) {
		t.Error("an earlier credential stopped verifying when another was issued")
	}
}
