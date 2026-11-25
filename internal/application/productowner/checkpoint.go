package productowner

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// A thread checkpoints when a turn ends with its context past a share of its
// model's window (ADR 0112): the product owner writes a summary in one more
// turn, the summary is stored with a checkpoint item that stands where the
// archived part ends, and the thread goes on in a new session, a fresh claude
// conversation whose brief carries the summary. The thread stays busy
// throughout, so a message sent meanwhile is refused and none is lost.

// summaryPrompt is the summary turn. The turn's answer is the summary.
const summaryPrompt = "Checkpoint: this conversation has grown long, and its earlier part will be archived. " +
	"Write a summary of it for yourself, to continue from in a fresh conversation: what the person asked, " +
	"what you found and said, the decisions made, what is still open, and the names of the tasks, documents " +
	"and threads involved. Answer with the summary only, and use no tools."

// specOf is what a thread's session starts or resumes with.
func specOf(t *domain.ProductOwnerThread) ports.ProductOwnerSessionSpec {
	return ports.ProductOwnerSessionSpec{ProjectID: t.ProjectID, AgentID: t.AgentID, Prompt: t.Prompt, Checkpoint: t.CheckpointSummary}
}

// clearSummarizing ends a summary turn that did not complete: the thread has
// not checkpointed, and the next turn may try again.
func clearSummarizing(th *domain.ProductOwnerThread) {
	if th.CheckpointPhase == domain.ProductOwnerCheckpointSummarizing {
		th.CheckpointPhase = ""
	}
}

// callContext is the tokens one API call read: its input and its cache tokens.
func callContext(u *ports.Usage) int {
	if u == nil {
		return 0
	}
	return u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
}

// contextSizeOf is the tokens the thread's context holds when a turn ends:
// what its last API call read, which claude reports on that call's message.
// The turn's result carries the usage summed over every call, which over-counts
// a turn that used tools, so it stands in only for a turn that reported no call
// usage.
func contextSizeOf(lastCall *ports.Usage, ev ports.SessionEvent) int {
	if lastCall != nil {
		return callContext(lastCall)
	}
	return callContext(ev.Usage)
}

// windowOf is the context window of the model a thread runs on: the one claude
// reported, and only while none was reported, a guess from the model's name.
func windowOf(model string, reported map[string]int) int {
	if w := reportedWindow(model, reported); w > 0 {
		return w
	}
	return domain.ContextWindow(model)
}

// reportedWindow picks the window of the thread's own model from what claude
// reported per model name. The thread names its model by an alias ("sonnet")
// and claude by the full name, so the entry whose name holds the alias is the
// thread's; a lone entry is the thread's whatever its name, and with several
// the largest stands for the main model, whose context is the one measured.
func reportedWindow(model string, reported map[string]int) int {
	best := 0
	for name, w := range reported {
		if model != "" && strings.Contains(name, model) && w > 0 {
			return w
		}
		best = max(best, w)
	}
	return best
}

// beginCheckpoint is called when a turn ends, with r.mu held. It starts the
// summary turn when the context is past the share, and says it did: the turn
// is not over then.
func (s *Service) beginCheckpoint(r *run, t *domain.ProductOwnerThread, ev ports.SessionEvent) bool {
	defer func() { r.lastCall = nil }() // the next turn measures its own calls
	window := windowOf(s.sessions.ProductOwnerModel(t.SessionID), ev.ContextWindows)
	if float64(contextSizeOf(r.lastCall, ev)) < float64(window)*domain.ProductOwnerCheckpointShare {
		return false
	}
	ctx := context.Background()
	if _, err := s.update(ctx, t.ID, nil, func(th *domain.ProductOwnerThread) bool {
		th.CheckpointPhase = domain.ProductOwnerCheckpointSummarizing
		return true
	}); err != nil {
		return false
	}
	r.summary.Reset()
	if err := s.sessions.Send(ctx, t.SessionID, summaryPrompt); err != nil {
		slog.Warn("ask a product owner thread for its checkpoint summary", "thread", t.ID, "err", err)
		_, _ = s.update(ctx, t.ID, nil, func(th *domain.ProductOwnerThread) bool { clearSummarizing(th); return true })
		return false
	}
	return true
}

// summarizing takes the events of a summary turn, with r.mu held, and says it
// took them. What the product owner says in it is the summary, not items; its
// end completes the checkpoint.
func (s *Service) summarizing(r *run, t *domain.ProductOwnerThread, ev ports.SessionEvent) bool {
	if t.CheckpointPhase != domain.ProductOwnerCheckpointSummarizing {
		return false
	}
	switch ev.Type {
	case "output_delta":
		if ev.DeltaKind == "text" {
			r.summary.WriteString(ev.Text)
		}
	case "output":
		if strings.TrimSpace(ev.Text) != "" {
			r.summary.Reset()
			r.summary.WriteString(ev.Text)
		}
	case "usage":
		r.lastCall = nil
		s.completeCheckpoint(r, t)
	}
	return true
}

// completeCheckpoint stores the summary with its checkpoint item and moves the
// thread to its new conversation, behind the handler: stopping the old process
// ends the stream the handler is reading.
func (s *Service) completeCheckpoint(r *run, t *domain.ProductOwnerThread) {
	ctx := context.Background()
	summary := strings.TrimSpace(r.summary.String())
	if summary == "" {
		s.failCheckpoint(ctx, t, r, "The product owner wrote no summary.")
		return
	}
	item, err := s.store.AppendCheckpoint(ctx, domain.ProductOwnerItem{ThreadID: t.ID, Kind: domain.ProductOwnerItemCheckpoint, At: time.Now(), Summary: summary})
	if err != nil {
		s.failCheckpoint(ctx, t, r, "The summary could not be stored: "+err.Error())
		return
	}
	s.announceItem(t, item)
	s.expireHeldActionsAtCheckpoint(ctx, t)
	r.retired = true
	s.setWaking(t.ID, true)
	go s.moveToNewConversation(r)
}

// failCheckpoint ends a checkpoint that could not be made: the thread is as it
// was, idle, with a failure item, and the next turn tries again.
func (s *Service) failCheckpoint(ctx context.Context, t *domain.ProductOwnerThread, r *run, text string) {
	// The person's last message is answered already: there is nothing to send again.
	s.fail(ctx, t, "", "CHECKPOINT_FAILED", text)
	s.resetIdle(r, true)
}

// expireHeldActionsAtCheckpoint expires the held actions of the thread still
// pending at its checkpoint: they were asked in the archived part, and the
// product owner's next turn is told they expired (spec story 10, ADR 0110).
func (s *Service) expireHeldActionsAtCheckpoint(ctx context.Context, t *domain.ProductOwnerThread) {
	if _, err := s.ExpirePending(ctx, t.ID); err != nil {
		slog.Warn("expire the held actions of a checkpointed thread", "thread", t.ID, "err", err)
	}
}

// moveToNewConversation ends the thread's old process and starts its new
// session, with the summary in its brief. It runs under the thread's lock, so
// a stop waits for it. A start that fails leaves the move to be done again
// by the thread's next message.
func (s *Service) moveToNewConversation(old *run) {
	defer s.setWaking(old.threadID, false)
	lock := s.threadLock(old.threadID)
	lock.Lock()
	defer lock.Unlock()
	ctx := context.Background()
	t, err := s.store.Thread(ctx, old.threadID)
	if err != nil || t.CheckpointPhase != domain.ProductOwnerCheckpointMoving {
		return
	}
	s.dropRun(old)
	_ = s.finishMove(ctx, t)
}

// finishMove ends the thread's old process, if one runs, and starts its new
// session with the stored summary in its brief. The caller holds the thread's
// lock. A start that fails says so in the thread and leaves the thread idle,
// still owing its move: the next message does it first.
func (s *Service) finishMove(ctx context.Context, t *domain.ProductOwnerThread) error {
	if s.sessions.ProductOwnerRunning(t.SessionID) {
		_ = s.sessions.Stop(ctx, t.SessionID)
		s.waitProcessEnd(ctx, t.SessionID)
	}
	sessionID, err := s.sessions.StartProductOwnerSession(ctx, specOf(t))
	if err != nil {
		slog.Warn("start a product owner thread's new conversation", "thread", t.ID, "err", err)
		s.fail(ctx, t, "", "SESSION_RESUME_FAILED", "The product owner could not start its new conversation: "+err.Error())
		return err
	}
	cur, err := s.update(ctx, t.ID, t, func(th *domain.ProductOwnerThread) bool {
		th.SessionID, th.CheckpointPhase = sessionID, ""
		// The new conversation knows nothing of the memory: its first message is
		// handed the whole of it again (ADR 0111, ADR 0112).
		th.MemoryOpened, th.MemorySeen = false, 0
		th.Turn, th.LastActivityAt = domain.ProductOwnerTurnIdle, time.Now()
		return true
	})
	if err != nil {
		_ = s.sessions.Stop(ctx, sessionID)
		return err
	}
	s.announceThread(*cur)
	s.resetIdle(s.attach(*cur), true)
	return nil
}

// resumeMove finishes a checkpoint a restart cut off after its summary was
// stored: the thread only has to move to its new conversation. The caller holds
// the thread's lock.
func (s *Service) resumeMove(ctx context.Context, t *domain.ProductOwnerThread) error {
	notice, err := s.store.AppendItem(ctx, domain.ProductOwnerItem{ThreadID: t.ID, Kind: domain.ProductOwnerItemNotice, At: time.Now(), Text: "Continued after a restart"})
	if err == nil {
		s.announceItem(t, notice)
	}
	return s.finishMove(ctx, t)
}
