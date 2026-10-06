package orchestration

import (
	"testing"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.SessionAnnouncer = (*Service)(nil)

// An announced artifact reaches a live subscriber of an *interactive* session
// and is in its persisted history, with the artifact embedded — the same
// path the session's own events take.
func TestAnnounce_ReachesSubscribersAndHistory(t *testing.T) {
	svc, _ := newInteractiveService(t)
	sess, _ := startInteractive(t, svc, InteractiveRequest{})
	ch, _, cancel := svc.Subscribe(sess.ID)
	defer cancel()

	art := &domain.Artifact{ID: "a1", SessionID: sess.ID, Kind: domain.ArtifactPage, Path: "design/card.html", Revision: 2}
	svc.Announce(sess.ID, SessionEvent{Type: "artifact", Text: "tighter spacing", Artifact: art})

	select {
	case ev := <-ch:
		if ev.Type != "artifact" || ev.Seq == 0 || ev.SessionID != sess.ID || ev.Artifact == nil || ev.Artifact.Revision != 2 {
			t.Fatalf("live event = %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no live artifact event")
	}
	var found bool
	for _, ev := range svc.History(sess.ID, 0) {
		if ev.Type == "artifact" && ev.Artifact != nil && ev.Artifact.ID == "a1" && ev.Text == "tighter spacing" {
			found = true
		}
	}
	if !found {
		t.Fatal("artifact event not in history")
	}
}

// Publishing changes nothing about the session record, so the project feed
// stays quiet; the session list has no reason to redraw.
func TestAnnounce_ArtifactIsQuietOnTheFeed(t *testing.T) {
	svc, _ := newInteractiveService(t)
	sess, _ := startInteractive(t, svc, InteractiveRequest{})
	mine := follow(t, svc, "p1")
	svc.Announce(sess.ID, SessionEvent{Type: "artifact", Artifact: &domain.Artifact{ID: "a1"}})
	select {
	case ch := <-mine:
		t.Fatalf("feed moved on an artifact event: %+v", ch)
	case <-time.After(200 * time.Millisecond):
	}
}
