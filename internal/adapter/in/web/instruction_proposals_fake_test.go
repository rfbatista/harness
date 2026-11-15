package web

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

// fakeInstructionProposals answers the endpoints of "Contract: Harness server ↔
// Web UI — instruction-file proposals" the way the server does, as far as the
// browser can tell: the proposals of repository "r1", their states, open_count,
// and the project feed's instruction_proposal_changed event. The browser tests
// of the proposals in the Instructions tab run against it until the server
// branch lands.
type fakeInstructionProposals struct {
	mu        sync.Mutex
	feed      *sseFeed
	proposals []*fakeProposal
	// refusals are the errors the next call of an endpoint answers with, by endpoint name; each is used once.
	refusals map[string]fakeRefusal
	calls    []string
	bodies   []map[string]any
	// clock numbers the proposals, so ids and the newest-first order are stable.
	clock int
}

type fakeProposal struct {
	ID      string
	State   string
	Summary string
	Skills  []string
	// Task and session that proposed it.
	TaskID, TaskTitle, Agent, Mode string
	Created                        time.Time
	Decided                        *time.Time
	Note                           string
}

func newFakeInstructionProposals(feed *sseFeed) *fakeInstructionProposals {
	return &fakeInstructionProposals{feed: feed, refusals: map[string]fakeRefusal{}}
}

var proposalRoutes = []string{"list_instruction_proposals"}

func (f *fakeInstructionProposals) register(mux *http.ServeMux) {
	for _, r := range proposalRoutes {
		mux.Handle("/api/"+r, f)
	}
}

// add stores a proposal without announcing it, as if an agent had submitted it earlier. Its id is p<n>; its age is how long ago it was proposed.
func (f *fakeInstructionProposals) add(p fakeProposal, age time.Duration) *fakeProposal {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.addLocked(p, age)
}

func (f *fakeInstructionProposals) addLocked(p fakeProposal, age time.Duration) *fakeProposal {
	f.clock++
	if p.ID == "" {
		p.ID = "p" + string(rune('0'+f.clock))
	}
	if p.State == "" {
		p.State = "open"
	}
	if p.TaskID == "" {
		p.TaskID, p.TaskTitle = "t1", "Verification Skill"
	}
	if p.Agent == "" {
		p.Agent, p.Mode = "software-architect", "architect"
	}
	p.Created = time.Now().Add(-age)
	if p.State != "open" {
		at := p.Created.Add(time.Minute)
		p.Decided = &at
	}
	f.proposals = append(f.proposals, &p)
	return &p
}

// submit is an agent's submit while the page is open: the proposal is stored and the feed announces it.
func (f *fakeInstructionProposals) submit(p fakeProposal) *fakeProposal {
	f.mu.Lock()
	stored := f.addLocked(p, 0)
	n := f.openLocked()
	id, state := stored.ID, stored.State
	f.mu.Unlock()
	f.emit(id, state, n)
	return stored
}

func (f *fakeInstructionProposals) openCount(_ context.Context, repositoryID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	if repositoryID != "r1" {
		return 0
	}
	return f.openLocked()
}

func (f *fakeInstructionProposals) openLocked() int {
	n := 0
	for _, p := range f.proposals {
		if p.State == "open" {
			n++
		}
	}
	return n
}

// emit sends instruction_proposal_changed to every follower of the project feed.
func (f *fakeInstructionProposals) emit(id, state string, open int) {
	b, _ := json.Marshal(map[string]any{"instruction_proposal_changed": map[string]any{"repository_id": "r1", "proposal_id": id, "state": state, "open_count": open}})
	f.feed.mu.Lock()
	defer f.feed.mu.Unlock()
	for _, c := range f.feed.conns {
		c <- b
	}
}

func (f *fakeInstructionProposals) called() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeInstructionProposals) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var in map[string]any
	_ = json.NewDecoder(r.Body).Decode(&in)
	name := strings.TrimPrefix(r.URL.Path, "/api/")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, name)
	f.bodies = append(f.bodies, in)
	if ref, ok := f.refusals[name]; ok {
		delete(f.refusals, name)
		f.fail(w, ref.Status, ref.Code, ref.Message, ref.Details)
		return
	}
	switch name {
	case "list_instruction_proposals":
		if id, _ := in["repository_id"].(string); id != "r1" {
			f.fail(w, http.StatusNotFound, "REPOSITORY_NOT_FOUND", "repository not found", nil)
			return
		}
		f.list(w)
	}
}

// shape is a proposal as the list answers it.
func (p *fakeProposal) shape() map[string]any {
	var decided, note any
	if p.Decided != nil {
		decided = p.Decided.Format(time.RFC3339)
	}
	if p.Note != "" {
		note = p.Note
	}
	skills := p.Skills
	if skills == nil {
		skills = []string{}
	}
	return map[string]any{
		"id": p.ID, "repository_id": "r1", "state": p.State, "summary": p.Summary, "skills": skills,
		"files":      1,
		"task":       map[string]any{"id": p.TaskID, "title": p.TaskTitle},
		"session":    map[string]any{"id": "s-" + p.ID, "agent": p.Agent, "mode": p.Mode},
		"created_at": p.Created.Format(time.RFC3339), "decided_at": decided, "note": note,
	}
}

// list answers every proposal, newest first.
func (f *fakeInstructionProposals) list(w http.ResponseWriter) {
	out := []any{}
	for i := len(f.proposals) - 1; i >= 0; i-- {
		out = append(out, f.proposals[i].shape())
	}
	f.json(w, map[string]any{"proposals": out, "open_count": f.openLocked()})
}

func (f *fakeInstructionProposals) json(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (f *fakeInstructionProposals) fail(w http.ResponseWriter, status int, code, message string, details any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body := map[string]any{"error": message, "code": code}
	if details != nil {
		body["details"] = details
	}
	_ = json.NewEncoder(w).Encode(body)
}
