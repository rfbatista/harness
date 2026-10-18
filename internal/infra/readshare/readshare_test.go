package readshare

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

// Within one scope, a read asked for under the same key runs once, however
// many callers ask at once, and each gets its answer.
func TestAReadIsSharedWithinItsScope(t *testing.T) {
	ctx := With(context.Background())
	var runs atomic.Int32
	read := func() (string, error) {
		runs.Add(1)
		return "clean", nil
	}

	var wg sync.WaitGroup
	got := make([]string, 8)
	for i := range got {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got[i], _ = Do(ctx, "status /w", read)
		}()
	}
	wg.Wait()

	if n := runs.Load(); n != 1 {
		t.Errorf("the read ran %d times, want once", n)
	}
	for i, g := range got {
		if g != "clean" {
			t.Errorf("caller %d got %q, want %q", i, g, "clean")
		}
	}
}

// Different keys are different reads, and a failed read shares its error.
func TestKeysAreReadApartAndErrorsAreShared(t *testing.T) {
	ctx := With(context.Background())
	boom := errors.New("boom")
	if _, err := Do(ctx, "a", func() (int, error) { return 0, boom }); !errors.Is(err, boom) {
		t.Fatalf("first read: err = %v, want boom", err)
	}
	if _, err := Do(ctx, "a", func() (int, error) { return 1, nil }); !errors.Is(err, boom) {
		t.Errorf("second read of a: err = %v, want the shared boom", err)
	}
	if n, err := Do(ctx, "b", func() (int, error) { return 2, nil }); n != 2 || err != nil {
		t.Errorf("read of b = %d, %v; want 2, nil", n, err)
	}
}

// Without a scope every read runs.
func TestWithoutAScopeEveryReadRuns(t *testing.T) {
	var runs int
	for range 3 {
		_, _ = Do(context.Background(), "k", func() (int, error) {
			runs++
			return runs, nil
		})
	}
	if runs != 3 {
		t.Errorf("the read ran %d times, want 3", runs)
	}
}

// A read that failed because its caller gave up (the caller's context ended)
// is not shared: the next caller, and one that was waiting for it, read
// again.
func TestAReadItsCallerGaveUpOnIsReadAgain(t *testing.T) {
	scoped := With(context.Background())
	first, giveUp := context.WithCancel(scoped)
	started, release := make(chan struct{}), make(chan struct{})
	go func() {
		<-started
		giveUp()
		close(release)
	}()

	waiter := make(chan error, 1)
	_, err := Do(first, "k", func() (int, error) {
		go func() {
			// A second caller joins while the first read runs.
			_, err := Do(scoped, "k", func() (int, error) { return 2, nil })
			waiter <- err
		}()
		close(started)
		<-release
		return 0, first.Err()
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("the first caller's read: err = %v, want canceled", err)
	}
	if err := <-waiter; err != nil {
		t.Errorf("the caller that waited got %v, want its own read", err)
	}
	if n, err := Do(scoped, "k", func() (int, error) { return 3, nil }); err != nil || n == 0 {
		t.Errorf("a later read = %d, %v; want a fresh answer", n, err)
	}
}
