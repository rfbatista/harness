package productowner

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// heldState is what the held actions of every thread share: the lock that
// keeps an answer, a hold and an expiry from counting one action twice, what
// applies a confirmed action, and the clock.
type heldState struct {
	mu      sync.Mutex
	applier ports.HeldActionApplier
	now     func() time.Time
}

var (
	_ ports.ProductOwnerHolds = (*Service)(nil)
)

// SetHeldActionApplier says what applies a confirmed held action.
func (s *Service) SetHeldActionApplier(a ports.HeldActionApplier) {
	s.held.mu.Lock()
	s.held.applier = a
	s.held.mu.Unlock()
}

// SetClock replaces the clock held actions expire by; tests use it.
func (s *Service) SetClock(now func() time.Time) {
	s.held.mu.Lock()
	s.held.now = now
	s.held.mu.Unlock()
}

func (s *Service) clock() time.Time {
	if s.held.now != nil {
		return s.held.now()
	}
	return time.Now()
}

func heldNotFound(id string) error {
	return &domain.StructuredError{Code: "HELD_ACTION_NOT_FOUND", Message: "HELD_ACTION_NOT_FOUND: no held action " + id}
}

// HoldAction implements ports.ProductOwnerHolds: the action is stored pending
// on the thread of the session and shown in it. Nothing is applied.
func (s *Service) HoldAction(ctx context.Context, sessionID string, action domain.HeldAction, summary string, target domain.HeldTarget) (domain.ProductOwnerItem, error) {
	t, err := s.store.ThreadOfSession(ctx, sessionID)
	if err != nil {
		return domain.ProductOwnerItem{}, err
	}
	if t == nil {
		return domain.ProductOwnerItem{}, &domain.StructuredError{Code: "THREAD_NOT_FOUND", Message: "THREAD_NOT_FOUND: this session has no product owner thread"}
	}
	s.held.mu.Lock()
	defer s.held.mu.Unlock()
	item, err := s.store.AppendItem(ctx, domain.ProductOwnerItem{ThreadID: t.ID, Kind: domain.ProductOwnerItemHeldAction, At: s.clock(),
		Summary: summary, Target: &target, State: domain.HeldActionPending, Held: &action})
	if err != nil {
		return item, err
	}
	item.HeldActionID = item.ID
	if err := s.store.SaveItem(ctx, item); err != nil {
		return item, err
	}
	if _, err := s.update(ctx, t.ID, t, func(th *domain.ProductOwnerThread) bool {
		th.PendingHeldActions++
		th.LastActivityAt = item.At
		return true
	}); err != nil {
		return item, err
	}
	s.announceItem(t, item)
	s.announceThread(*t)
	return item, nil
}

// AnswerHeldAction implements ports.ProductOwner. A confirm applies the
// change as the product owner of the thread; a decline applies nothing.
// Neither starts a turn: the product owner learns the answer on the next one.
func (s *Service) AnswerHeldAction(ctx context.Context, heldActionID, answer string) (*ports.HeldActionAnswer, error) {
	if answer != domain.HeldAnswerConfirm && answer != domain.HeldAnswerDecline {
		return nil, invalidInput("answer must be confirm or decline")
	}
	s.held.mu.Lock()
	defer s.held.mu.Unlock()
	item, err := s.store.Item(ctx, heldActionID)
	if err != nil {
		return nil, err
	}
	if item == nil || item.Kind != domain.ProductOwnerItemHeldAction {
		return nil, heldNotFound(heldActionID)
	}
	t, err := s.store.Thread(ctx, item.ThreadID)
	if err != nil {
		return nil, err
	}
	switch item.State {
	case domain.HeldActionConfirmed, domain.HeldActionDeclined:
		return nil, &domain.StructuredError{Code: "HELD_ACTION_ANSWERED", Message: "HELD_ACTION_ANSWERED: this action was already " + item.State}
	case domain.HeldActionExpired:
		return nil, expiredError()
	}
	if s.clock().Sub(item.At) >= domain.HeldActionTTL {
		s.settle(ctx, t, item, domain.HeldActionExpired)
		return nil, expiredError()
	}
	res := &ports.HeldActionAnswer{HeldActionID: item.ID}
	if answer == domain.HeldAnswerConfirm {
		if s.held.applier == nil || item.Held == nil {
			return nil, &domain.StructuredError{Code: "UNAVAILABLE", Message: "UNAVAILABLE: nothing here can apply this action"}
		}
		by := domain.Author{Kind: domain.AuthorProductOwner, SessionID: t.SessionID, ThreadID: t.ID}
		if res.Outcome, err = s.held.applier.ApplyHeldAction(ctx, by, *item.Held); err != nil {
			if errors.Is(err, ports.ErrHeldActionStale) {
				// The task moved on: the action no longer applies, and says so.
				s.settle(ctx, t, item, domain.HeldActionExpired)
				return nil, expiredError()
			}
			return nil, err
		}
		if err := s.settle(ctx, t, item, domain.HeldActionConfirmed); err != nil {
			return nil, err
		}
	} else {
		if err := s.settle(ctx, t, item, domain.HeldActionDeclined); err != nil {
			return nil, err
		}
	}
	res.State = item.State
	return res, nil
}

func expiredError() error {
	return &domain.StructuredError{Code: "HELD_ACTION_EXPIRED", Message: "HELD_ACTION_EXPIRED: this action was not answered in time and did not happen"}
}

// settle moves a pending item to state, takes it off the thread's pending
// count and shows both. The held lock is held.
func (s *Service) settle(ctx context.Context, t *domain.ProductOwnerThread, item *domain.ProductOwnerItem, state string) error {
	item.State = state
	if err := s.store.SaveItem(ctx, *item); err != nil {
		return err
	}
	if _, err := s.update(ctx, t.ID, t, func(th *domain.ProductOwnerThread) bool {
		if th.PendingHeldActions > 0 {
			th.PendingHeldActions--
		}
		return true
	}); err != nil {
		return err
	}
	s.announceItem(t, *item)
	s.announceThread(*t)
	return nil
}

// ExpirePending expires every pending held action of a thread, as its
// checkpoint does: the thread's context is archived, so an action asked
// before it can no longer be answered. It answers how many it expired.
func (s *Service) ExpirePending(ctx context.Context, threadID string) (int, error) {
	return s.expire(ctx, func(item domain.ProductOwnerItem) bool { return item.ThreadID == threadID })
}

// ExpireStale expires every pending held action older than a day, in every
// thread.
func (s *Service) ExpireStale(ctx context.Context) (int, error) {
	now := s.clock()
	return s.expire(ctx, func(item domain.ProductOwnerItem) bool { return now.Sub(item.At) >= domain.HeldActionTTL })
}

func (s *Service) expire(ctx context.Context, which func(domain.ProductOwnerItem) bool) (int, error) {
	s.held.mu.Lock()
	defer s.held.mu.Unlock()
	pending, err := s.store.PendingHeldActions(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for i := range pending {
		if !which(pending[i]) {
			continue
		}
		t, err := s.store.Thread(ctx, pending[i].ThreadID)
		if err != nil {
			continue
		}
		s.settle(ctx, t, &pending[i], domain.HeldActionExpired)
		n++
	}
	return n, nil
}

// WatchHeldActions expires the held actions that waited a day, once a minute,
// until ctx ends.
func (s *Service) WatchHeldActions(ctx context.Context) {
	go func() {
		tick := time.NewTicker(time.Minute)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				_, _ = s.ExpireStale(ctx)
			}
		}
	}()
}

// heldAnswers are the lines that tell the product owner what the person
// answered since its last turn, and the items they come from.
func (s *Service) heldAnswers(ctx context.Context, threadID string) (string, []domain.ProductOwnerItem) {
	told, err := s.store.AnsweredHeldActions(ctx, threadID)
	if err != nil {
		return "", nil
	}
	var b strings.Builder
	for _, it := range told {
		b.WriteString("- " + strings.ReplaceAll(it.Summary, `"`, "'") + ": the person " + heldOutcome[it.State] + "\n")
	}
	if len(told) == 0 {
		return "", nil
	}
	return "[Since your last turn, the answers to what you asked the person to confirm:\n" + b.String() + "]\n\n", told
}

var heldOutcome = map[string]string{
	domain.HeldActionConfirmed: "confirmed, and it was done.",
	domain.HeldActionDeclined:  "declined, so nothing changed.",
	domain.HeldActionExpired:   "did not answer in time, so nothing changed.",
}

// markTold records that the product owner has been given these answers.
func (s *Service) markTold(ctx context.Context, items []domain.ProductOwnerItem) {
	s.held.mu.Lock()
	defer s.held.mu.Unlock()
	for _, it := range items {
		if cur, err := s.store.Item(ctx, it.ID); err == nil && cur != nil && cur.Held != nil {
			cur.Held.Told = true
			_ = s.store.SaveItem(ctx, *cur)
		}
	}
}
