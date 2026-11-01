package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"operators-mcp/internal/ports"
)

// fakeAssistant answers the two endpoints of "Contract: Harness server ↔ Web
// UI — architecture assistant turns" the way the server does, as far as the
// browser can tell: a streamed turn of SSE-framed events, the refusals before
// the stream, and the end request. Every other path goes to the repository
// architecture fake. Each turn runs the next script; a script writes the
// turn's events and may hold the stream open until the browser aborts it.
type fakeAssistant struct {
	*fakeArchitectureAPI
	amu      sync.Mutex
	scripts  []func(t *assistantTurn)
	requests []map[string]any
	ended    []string
	// aborted counts the turns whose request the browser cancelled.
	aborted int
	nextID  int
}

// assistantTurn is what a script sees of one turn.
type assistantTurn struct {
	In map[string]any
	w  http.ResponseWriter
	r  *http.Request
	// ConversationID is the id the turn runs under (issued on the first turn).
	ConversationID string
	streaming      bool
}

func newFakeAssistant(api *fakeArchitectureAPI) *fakeAssistant {
	return &fakeAssistant{fakeArchitectureAPI: api}
}

// script queues what the next turn does.
func (f *fakeAssistant) script(s ...func(t *assistantTurn)) {
	f.amu.Lock()
	defer f.amu.Unlock()
	f.scripts = append(f.scripts, s...)
}

func (f *fakeAssistant) turns() []map[string]any {
	f.amu.Lock()
	defer f.amu.Unlock()
	return append([]map[string]any(nil), f.requests...)
}

func (f *fakeAssistant) endedIDs() []string {
	f.amu.Lock()
	defer f.amu.Unlock()
	return append([]string(nil), f.ended...)
}

func (f *fakeAssistant) abortedTurns() int {
	f.amu.Lock()
	defer f.amu.Unlock()
	return f.aborted
}

// Event writes one SSE event. The conversation event goes first, on its own.
func (t *assistantTurn) Event(name string, data any) {
	if !t.streaming {
		t.w.Header().Set("Content-Type", "text/event-stream")
		t.w.WriteHeader(http.StatusOK)
		t.streaming = true
		b, _ := json.Marshal(map[string]any{"conversation_id": t.ConversationID})
		fmt.Fprintf(t.w, "event: conversation\ndata: %s\n\n", b)
	}
	b, _ := json.Marshal(data)
	fmt.Fprintf(t.w, "event: %s\ndata: %s\n\n", name, b)
	t.w.(http.Flusher).Flush()
}

// Fail refuses before the stream with the server's JSON error body.
func (t *assistantTurn) Fail(status int, code, message string, extra map[string]any) {
	body := map[string]any{"error": message, "code": code}
	for k, v := range extra {
		body[k] = v
	}
	t.w.Header().Set("Content-Type", "application/json")
	t.w.WriteHeader(status)
	_ = json.NewEncoder(t.w).Encode(body)
}

// Hold waits until the browser aborts the request and reports whether it did, or until d passes.
func (t *assistantTurn) Hold(d time.Duration) bool {
	select {
	case <-t.r.Context().Done():
		return true
	case <-time.After(d):
		return false
	}
}

// answer is the script of a turn that streams text and ends with a plain answer.
func answer(text string) func(t *assistantTurn) {
	return func(t *assistantTurn) {
		for _, word := range strings.SplitAfter(text, " ") {
			t.Event("delta", map[string]any{"text": word})
		}
		t.Event("result", map[string]any{"text": text, "proposal": nil})
	}
}

func (f *fakeAssistant) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/end_architecture_assistant_conversation":
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.amu.Lock()
		id, _ := in["conversation_id"].(string)
		f.ended = append(f.ended, id)
		f.amu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	case "/api/run_architecture_assistant_turn":
		f.turn(w, r)
	default:
		f.fakeArchitectureAPI.ServeHTTP(w, r)
	}
}

func (f *fakeAssistant) turn(w http.ResponseWriter, r *http.Request) {
	var in map[string]any
	_ = json.NewDecoder(r.Body).Decode(&in)
	f.amu.Lock()
	f.requests = append(f.requests, in)
	var script func(t *assistantTurn)
	if len(f.scripts) > 0 {
		script, f.scripts = f.scripts[0], f.scripts[1:]
	}
	id, _ := in["conversation_id"].(string)
	if id == "" {
		f.nextID++
		id = fmt.Sprintf("c%d", f.nextID)
	}
	f.amu.Unlock()

	t := &assistantTurn{In: in, w: w, r: r, ConversationID: id}
	if script == nil {
		t.Fail(http.StatusInternalServerError, "ARCHITECTURE_ASSISTANT_FAILED", "no script for this turn", nil)
		return
	}
	script(t)
	if r.Context().Err() != nil {
		f.amu.Lock()
		f.aborted++
		f.amu.Unlock()
	}
}

// fakeReadiness is the readiness the page handler reads for the assistant's availability, reason and
// fix URL: the page seed is the server's own, not a rewrite of it.
type fakeReadiness struct {
	ports.ArchitectureAssistantAvailability
}

func (f fakeReadiness) Availability(context.Context, string) ports.ArchitectureAssistantAvailability {
	return f.ArchitectureAssistantAvailability
}
func (f fakeReadiness) Readiness(context.Context) ports.ArchitectureAssistantAvailability {
	return f.ArchitectureAssistantAvailability
}

// newAssistantRig is newArchRig with the assistant's endpoints and the page's own seed attributes. A
// turn with no script fails, so a test scripts every turn it sends.
func newAssistantRig(t *testing.T, available bool, reason, fixURL string) (*archRig, *fakeAssistant) {
	t.Helper()
	var fake *fakeAssistant
	readiness := fakeReadiness{ports.ArchitectureAssistantAvailability{Available: available, Reason: reason, FixURL: fixURL}}
	rig := newArchRigFull(t, nil, nil, func(api *fakeArchitectureAPI, pages http.Handler) (http.Handler, http.Handler) {
		fake = newFakeAssistant(api)
		return fake, pages
	}, readiness)
	return rig, fake
}
