package orchestration

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/persistence/memory"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

func TestContextCoalesce_AChangeExactlyOneWindowAfterTheLastMessageIsSentAtOnce(t *testing.T) {
	clock := newFakeClock()
	n, sent := notifierWith(clock)
	n.changed("s1", ports.ContextUsage)
	clock.Advance(contextWindow - time.Millisecond)
	n.changed("s1", ports.ContextUsage)
	if len(*sent) != 1 {
		t.Fatalf("1 ms inside the window: %v", *sent)
	}
	clock.Advance(time.Millisecond) // the timer fires at exactly 2 s
	if len(*sent) != 2 {
		t.Fatalf("at 2 s: %v", *sent)
	}
	clock.Advance(contextWindow)
	n.changed("s1", ports.ContextUsage)
	if len(*sent) != 3 {
		t.Errorf("exactly 2 s after the last message: %v", *sent)
	}
}

func TestContextCoalesce_TheMostTellingReasonOfABurstWinsWhateverTheOrder(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want string
	}{
		{"entry then usage", []string{"entry_added", "usage"}, "entry_added"},
		{"usage then entry", []string{"usage", "entry_added"}, "entry_added"},
		{"mcp then usage", []string{"mcp_status", "usage"}, "mcp_status"},
		{"entry then mcp", []string{"entry_added", "mcp_status"}, "entry_added"},
		{"usage only", []string{"usage", "usage", "usage"}, "usage"},
	}
	for _, c := range cases {
		clock := newFakeClock()
		n, sent := notifierWith(clock)
		n.changed("s1", "usage") // opens the window
		for _, r := range c.in {
			n.changed("s1", r)
		}
		clock.Advance(contextWindow)
		if len(*sent) != 2 || (*sent)[1].reason != c.want {
			t.Errorf("%s: sent %v, want the second to be %q", c.name, *sent, c.want)
		}
	}
}

func TestContextCoalesce_AWindowWithNothingWaitingSendsNothingAtItsEnd(t *testing.T) {
	clock := newFakeClock()
	n, sent := notifierWith(clock)
	n.changed("s1", "usage")
	clock.Advance(10 * contextWindow)
	if len(*sent) != 1 {
		t.Errorf("sent = %v", *sent)
	}
}

func TestContextCoalesce_AChangeRightAfterACoalescedMessageStartsANewWindow(t *testing.T) {
	clock := newFakeClock()
	n, sent := notifierWith(clock)
	n.changed("s1", "usage")
	clock.Advance(time.Second)
	n.changed("s1", "usage")
	clock.Advance(time.Second) // flush at 2 s
	if len(*sent) != 2 {
		t.Fatalf("sent = %v", *sent)
	}
	clock.Advance(100 * time.Millisecond)
	n.changed("s1", "usage") // 0.1 s after the flushed message
	if len(*sent) != 2 {
		t.Errorf("a change 0.1 s after a message was sent at once: %v", *sent)
	}
	clock.Advance(contextWindow)
	if len(*sent) != 3 {
		t.Errorf("the waiting change never went: %v", *sent)
	}
}

func TestContextCoalesce_ASteadyStreamIsNeverFasterThanOnePerWindowAndTheLastChangeArrives(t *testing.T) {
	clock := newFakeClock()
	var at []time.Time
	var reasons []string
	n := newContextNotifier(clock.Now, clock.AfterFunc, func(_, reason string) { at = append(at, clock.Now()); reasons = append(reasons, reason) })
	for i := 0; i < 100; i++ { // a change every 100 ms for 10 s
		r := "usage"
		if i == 99 {
			r = "mcp_status"
		}
		n.changed("s1", r)
		clock.Advance(100 * time.Millisecond)
	}
	clock.Advance(contextWindow)
	for i := 1; i < len(at); i++ {
		if gap := at[i].Sub(at[i-1]); gap < contextWindow {
			t.Errorf("messages %d and %d are %v apart", i-1, i, gap)
		}
	}
	if len(at) < 4 || len(at) > 7 {
		t.Errorf("%d messages for 10 s of changes", len(at))
	}
	if reasons[len(reasons)-1] != "mcp_status" {
		t.Errorf("the last change was lost: %v", reasons)
	}
}

func TestContextCoalesce_AWholeBurstSetsOneTimerNotOnePerChange(t *testing.T) {
	clock := newFakeClock()
	n, _ := notifierWith(clock)
	for i := 0; i < 50; i++ {
		n.changed("s1", "usage")
	}
	if len(clock.timers) != 1 {
		t.Errorf("timers = %d", len(clock.timers))
	}
}

// The service wiring: what reaches a project's followers.

func serviceWithClock(t *testing.T) (*Service, *fakeClock) {
	t.Helper()
	svc := contextService(t)
	clock := newFakeClock()
	svc.contextNotes = newContextNotifier(clock.Now, clock.AfterFunc, svc.sendContextChange)
	return svc, clock
}

func TestContextCoalesce_OnlyTheFollowersOfTheSessionsProjectHearIt(t *testing.T) {
	svc, _ := serviceWithClock(t)
	mine := follow(t, svc, "p1")
	other := follow(t, svc, "p2")
	storeEntry(t, svc, serversEntry())
	if ch := next(t, mine); ch.ContextChange == nil {
		t.Fatalf("change = %+v", ch)
	}
	select {
	case ch := <-other:
		t.Errorf("another project's follower heard %+v", ch)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestContextCoalesce_ASessionWithNoProjectOrNoRecordTellsNoOne(t *testing.T) {
	svc, _ := serviceWithClock(t)
	feed := follow(t, svc, "p1")
	svc.sendContextChange("ghost", ports.ContextUsage)
	if _, err := svc.sessions.Create(&domain.Session{ID: "loose", Task: "t", Status: domain.SessionRunning}); err != nil {
		t.Fatal(err)
	}
	svc.sendContextChange("loose", ports.ContextUsage)
	select {
	case ch := <-feed:
		t.Errorf("heard %+v", ch)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestContextCoalesce_ChangesOfTwoSessionsOfOneProjectEachGetTheirOwnMessage(t *testing.T) {
	svc, _ := serviceWithClock(t)
	if _, err := svc.sessions.Create(&domain.Session{ID: "s2", ProjectID: "p1", Task: "t", Status: domain.SessionRunning}); err != nil {
		t.Fatal(err)
	}
	feed := follow(t, svc, "p1")
	svc.contextChanged("s1", ports.ContextUsage)
	svc.contextChanged("s2", ports.ContextUsage)
	svc.contextChanged("s1", ports.ContextUsage)
	got := map[string]int{}
	for i := 0; i < 2; i++ {
		got[next(t, feed).ContextChange.SessionID]++
	}
	if got["s1"] != 1 || got["s2"] != 1 {
		t.Errorf("heard %v", got)
	}
}

func TestContextCoalesce_AChangeThatChangesNothingSendsNothing(t *testing.T) {
	svc, clock := serviceWithClock(t)
	storeEntry(t, svc, serversEntry())
	tr := &scriptedTranscript{}
	svc.TranscriptReader = tr
	feed := follow(t, svc, "p1")
	clock.Advance(10 * time.Second)

	applyInit(t, svc, `{"claude_code_version":"2.1.0"}`, nil)                  // a version alone
	applyInit(t, svc, `{"mcp_servers":[{"name":"x","status":"failed"}]}`, nil) // a server the session lacks
	applyInit(t, svc, `not json`, nil)
	tr.add(clock.Now(), ports.TranscriptUser, "", "hi") // no tool call
	syncUsage(t, svc)
	syncUsage(t, svc) // nothing new
	select {
	case ch := <-feed:
		t.Errorf("heard %+v for a pass that changed nothing", ch)
	case <-time.After(150 * time.Millisecond):
	}
	clock.Advance(10 * time.Second)
	select {
	case ch := <-feed:
		t.Errorf("heard %+v after the window", ch)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestContextCoalesce_ARepeatedInitReportSendsNoSecondMessage(t *testing.T) {
	svc, clock := serviceWithClock(t)
	storeEntry(t, svc, serversEntry())
	feed := follow(t, svc, "p1")
	clock.Advance(10 * time.Second)
	report := initOf([3]string{"task", "connected", ""})
	applyInit(t, svc, report, nil)
	if got := next(t, feed).ContextChange.Reason; got != "mcp_status" {
		t.Fatalf("reason = %q", got)
	}
	clock.Advance(10 * time.Second)
	applyInit(t, svc, report, nil)
	clock.Advance(10 * time.Second)
	select {
	case ch := <-feed:
		t.Errorf("the same report again told the page: %+v", ch)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestContextCoalesce_AnEntryThatCannotBeStoredTellsNoOne(t *testing.T) {
	svc, _ := serviceWithClock(t)
	svc.Contexts = &failingStore{SessionContextStore: memory.NewSessionContextStore()}
	feed := follow(t, svc, "p1")
	if err := svc.recordEntry(context.Background(), "s1", serversEntry()); err == nil {
		t.Fatal("expected the store's error")
	}
	select {
	case ch := <-feed:
		t.Errorf("heard %+v for an entry that was not stored", ch)
	case <-time.After(150 * time.Millisecond):
	}
}

type failingStore struct{ *memory.SessionContextStore }

func (failingStore) Save(context.Context, *domain.SessionContext) error { return errNoConversation }

func TestContextCoalesce_EveryReasonIsSentWithExactlyTheContractsThreeKeys(t *testing.T) {
	for _, reason := range []string{ports.ContextEntryAdded, ports.ContextUsage, ports.ContextMCPStatus} {
		b, err := json.Marshal(ports.ProjectChange{ContextChange: &ports.ContextChange{
			Kind: ports.ContextChangeKind, SessionID: "s1", Reason: reason, ProjectID: "p1"}})
		if err != nil {
			t.Fatal(err)
		}
		var wire map[string]any
		_ = json.Unmarshal(b, &wire)
		if len(wire) != 3 || wire["kind"] != "context.changed" || wire["session_id"] != "s1" || wire["reason"] != reason {
			t.Errorf("%s: wire = %s", reason, b)
		}
	}
	if ports.ContextEntryAdded != "entry_added" || ports.ContextUsage != "usage" || ports.ContextMCPStatus != "mcp_status" {
		t.Error("a reason's value changed: the contract fixes them")
	}
}

func TestContextCoalesce_TheServicesOwnNotifierHoldsBackASecondChangeForTwoSeconds(t *testing.T) {
	svc := contextService(t) // the notifier NewService builds, on the real clock
	feed := follow(t, svc, "p1")
	storeEntry(t, svc, entryWith("e1", domain.ContextEntryStart, time.Now()))
	first := time.Now()
	if got := next(t, feed).ContextChange.Reason; got != "entry_added" {
		t.Fatalf("reason = %q", got)
	}
	storeEntry(t, svc, entryWith("e2", domain.ContextEntryResume, time.Now()))
	select {
	case ch := <-feed:
		t.Fatalf("a second change inside the window was sent at once: %+v", ch)
	case <-time.After(500 * time.Millisecond):
	}
	next(t, feed)
	if gap := time.Since(first); gap < 1800*time.Millisecond {
		t.Errorf("the second message came %v after the first, want about 2 s", gap)
	}
	select {
	case ch := <-feed:
		t.Errorf("one more message: %+v", ch)
	case <-time.After(300 * time.Millisecond):
	}
}
