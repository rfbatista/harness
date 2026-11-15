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
	mu    sync.Mutex
	feed  *sseFeed
	files *fakeInstructionFiles
	// approveDelay holds approve's answer back after the proposal is decided and announced, as a slow server does.
	approveDelay time.Duration
	proposals    []*fakeProposal
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
	// Files are what it would change; a proposal with none still answers a file count of one.
	Files []fakeProposalFile
}

// fakeProposalFile is one file of a proposal: its op and the version the draft read. The base content is what the draft read; Content is the proposed text.
type fakeProposalFile struct {
	Path, Op             string
	BaseVersion          int
	BaseContent, Content string
}

func newFakeInstructionProposals(feed *sseFeed, files *fakeInstructionFiles) *fakeInstructionProposals {
	return &fakeInstructionProposals{feed: feed, files: files, refusals: map[string]fakeRefusal{}}
}

var proposalRoutes = []string{"list_instruction_proposals", "get_instruction_proposal", "approve_instruction_proposal", "reject_instruction_proposal", "send_back_instruction_proposal"}

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

// emitAsync is emit for a caller holding f.mu: the feed has its own lock, so it only names the intent.
func (f *fakeInstructionProposals) emitAsync(id, state string, open int) { f.emit(id, state, open) }

// approveCalls is how many times approve_instruction_proposal was asked.
func (f *fakeInstructionProposals) approveCalls() int {
	return f.callsTo("approve_instruction_proposal")
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
	case "get_instruction_proposal":
		f.get(w, in)
	case "approve_instruction_proposal":
		f.approve(w, in)
	case "reject_instruction_proposal":
		f.decide(w, in, "rejected", false)
	case "send_back_instruction_proposal":
		f.decide(w, in, "sent_back", true)
	}
}

// decide rejects or sends back; a send back needs its note.
func (f *fakeInstructionProposals) decide(w http.ResponseWriter, in map[string]any, state string, noteRequired bool) {
	p := f.find(in)
	note, _ := in["note"].(string)
	switch {
	case p == nil:
		f.fail(w, http.StatusNotFound, "PROPOSAL_NOT_FOUND", "no such proposal", nil)
		return
	case p.State != "open":
		f.fail(w, http.StatusConflict, "PROPOSAL_DECIDED", "the proposal is already "+p.State, nil)
		return
	case noteRequired && strings.TrimSpace(note) == "":
		f.fail(w, http.StatusBadRequest, "INVALID_INPUT", "a note is required", nil)
		return
	}
	at := time.Now()
	p.State, p.Decided, p.Note = state, &at, note
	f.emit(p.ID, p.State, f.openLocked())
	f.json(w, map[string]any{"proposal": p.shape()})
}

// setStateQuietly is a decision made elsewhere that the feed has not told this page about yet.
func (f *fakeInstructionProposals) setStateQuietly(id, state string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.proposals {
		if p.ID == id {
			at := time.Now()
			p.State, p.Decided = state, &at
		}
	}
}

// callsTo is how many times an endpoint was asked.
func (f *fakeInstructionProposals) callsTo(endpoint string) int {
	n := 0
	for _, c := range f.called() {
		if c == endpoint {
			n++
		}
	}
	return n
}

// lastBody is the request body of the newest call of an endpoint, or nil.
func (f *fakeInstructionProposals) lastBody(endpoint string) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.calls) - 1; i >= 0; i-- {
		if f.calls[i] == endpoint {
			return f.bodies[i]
		}
	}
	return nil
}

func (f *fakeInstructionProposals) find(in map[string]any) *fakeProposal {
	id, _ := in["proposal_id"].(string)
	for _, p := range f.proposals {
		if p.ID == id {
			return p
		}
	}
	return nil
}

// fileShape is a file of a proposal as get answers it: stale when the file's version moved on since the draft read it.
func (f *fakeInstructionProposals) fileShape(pf fakeProposalFile) map[string]any {
	current := f.files.version(pf.Path)
	var base, content any
	if pf.Op != "create" {
		base = pf.BaseContent
	}
	if pf.Op != "remove" {
		content = pf.Content
	}
	return map[string]any{
		"path": pf.Path, "op": pf.Op, "base_version": pf.BaseVersion, "current_version": current,
		"stale": current != pf.BaseVersion, "base_content": base, "content": content,
	}
}

func (f *fakeInstructionProposals) get(w http.ResponseWriter, in map[string]any) {
	p := f.find(in)
	if p == nil {
		f.fail(w, http.StatusNotFound, "PROPOSAL_NOT_FOUND", "no such proposal", nil)
		return
	}
	files := []any{}
	for _, pf := range p.Files {
		files = append(files, f.fileShape(pf))
	}
	f.json(w, map[string]any{"proposal": p.shape(), "files": files})
}

// approve saves every file in one go, or none: a stale file refuses the whole proposal.
func (f *fakeInstructionProposals) approve(w http.ResponseWriter, in map[string]any) {
	p := f.find(in)
	switch {
	case p == nil:
		f.fail(w, http.StatusNotFound, "PROPOSAL_NOT_FOUND", "no such proposal", nil)
		return
	case p.State != "open":
		f.fail(w, http.StatusConflict, "PROPOSAL_DECIDED", "the proposal is already "+p.State, nil)
		return
	}
	var stale []string
	for _, pf := range p.Files {
		if f.files.version(pf.Path) != pf.BaseVersion {
			stale = append(stale, pf.Path)
		}
	}
	if len(stale) > 0 {
		f.fail(w, http.StatusConflict, "INSTRUCTION_CHANGED", "files changed since the draft read them", map[string]any{"paths": stale})
		return
	}
	saved := []any{}
	for _, pf := range p.Files {
		if pf.Op == "remove" {
			f.files.deleteElsewhere(pf.Path)
			continue
		}
		f.files.saveElsewhereAs(pf.Path, kindOfPath(pf.Path), pf.Content)
		saved = append(saved, f.files.metaOf(pf.Path))
	}
	at := time.Now()
	p.State, p.Decided = "approved", &at
	open := f.openLocked()
	f.emitAsync(p.ID, p.State, open)
	if f.approveDelay > 0 {
		// Answer late, after the feed has told every tab: the person's own echo beats the POST's answer.
		f.mu.Unlock()
		time.Sleep(f.approveDelay)
		f.mu.Lock()
	}
	f.json(w, map[string]any{"proposal": p.shape(), "files": saved})
}

// kindOfPath is the instruction kind a file's path implies.
func kindOfPath(path string) string {
	switch {
	case strings.HasPrefix(path, ".claude/skills/"):
		return "skill"
	case strings.HasPrefix(path, ".claude/agents/"):
		return "subagent"
	}
	return "memory"
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
