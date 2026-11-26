package personproof

import (
	"context"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/domain"
)

type memStore map[string]bool

func (m memStore) Add(_ context.Context, h string) error         { m[h] = true; return nil }
func (m memStore) Has(_ context.Context, h string) (bool, error) { return m[h], nil }
func (m memStore) Count(context.Context) (int, error)            { return len(m), nil }

func TestAPairingCodeIsRedeemedOnceForACredentialThatVerifiesAndOnlyItsHashIsStored(t *testing.T) {
	store := memStore{}
	s := New(store)
	code, ok := s.FirstCode(context.Background())
	if !ok || code == "" {
		t.Fatal("no first code with nobody paired")
	}
	cred, err := s.Redeem(context.Background(), code)
	if err != nil || len(cred) < 32 {
		t.Fatalf("redeem = %q, %v", cred, err)
	}
	if !s.Verify(context.Background(), cred) || s.Verify(context.Background(), "wrong") || s.Verify(context.Background(), "") {
		t.Error("verify is wrong")
	}
	for h := range store {
		if strings.Contains(h, cred) || h == cred {
			t.Errorf("the store holds the credential itself: %q", h)
		}
	}
	// A second use, and a wrong code, are refused.
	for name, c := range map[string]string{"second use": code, "wrong": "ABC", "empty": ""} {
		if _, err := s.Redeem(context.Background(), c); err == nil || err.(*domain.StructuredError).Code != "PAIRING_CODE_INVALID" {
			t.Errorf("%s: %v", name, err)
		}
	}
	// Someone is paired: no first code is printed again.
	if _, ok := s.FirstCode(context.Background()); ok {
		t.Error("a first code was offered with a credential issued")
	}
}

func TestACodeExpiresAndANewCodeReplacesTheOldOne(t *testing.T) {
	s := New(memStore{})
	now := time.Unix(1000, 0)
	s.SetClock(func() time.Time { return now })
	old := s.NewCode()
	fresh := s.NewCode()
	if _, err := s.Redeem(context.Background(), old); err == nil {
		t.Error("a replaced code worked")
	}
	now = now.Add(CodeTTL + time.Second)
	if _, err := s.Redeem(context.Background(), fresh); err == nil {
		t.Error("an expired code worked")
	}
}

func TestACredentialIssuedBeforeARestartStillVerifiesAndACodeFromBeforeItDoesNot(t *testing.T) {
	store := memStore{}
	first := New(store)
	code := first.NewCode()
	cred, _ := first.Redeem(context.Background(), code)

	restarted := New(store) // same store, a new process: the code is gone
	if !restarted.Verify(context.Background(), cred) {
		t.Error("a credential did not survive a restart")
	}
	if _, err := restarted.Redeem(context.Background(), first.NewCode()); err != nil {
		// first.NewCode() belongs to the old process's memory, not restarted's.
		_ = err
	} else {
		t.Error("a code from the old process worked in the new one")
	}
}
