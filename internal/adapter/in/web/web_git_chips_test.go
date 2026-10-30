package web

import (
	"context"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

// stateAt answers the one workspace state it holds, for the pages that read a
// single worktree (the session history's head) and the Code tab's task read.
func (f *fakeGitStates) GitState(_ context.Context, workspaceID string) (*domain.GitState, error) {
	for _, states := range f.byTask {
		for i := range states {
			if states[i].WorkspaceID == workspaceID {
				return &states[i], f.err
			}
		}
	}
	return nil, f.err
}

func aheadState(workspace, session, branch string) domain.GitState {
	ahead, behind := 3, 0
	ref, remote := "origin/"+branch, "origin"
	return domain.GitState{
		WorkspaceID: workspace, SessionID: session, Branch: branch,
		Base:     domain.BaseState{Ref: "main", Ahead: 3, Behind: 8},
		Upstream: domain.UpstreamState{State: domain.UpstreamAhead, Ref: &ref, Remote: &remote, Ahead: &ahead, Behind: &behind},
	}
}

var chipPattern = regexp.MustCompile(`(?s)<span class="sync-state" data-state="ahead"[^>]*>.*?</span></span></span>`)

// The first paint of the task page carries each session row's chip, before any
// script runs: the remote's name, the counts with their words, no flash.
func TestTaskPagePaintsEachRowsSyncChipOnTheFirstPaint(t *testing.T) {
	w := board()
	w.sessions[0].WorkspaceID = "w1"
	gone := domain.GitState{WorkspaceID: "w2", SessionID: w.sessions[1].ID, Gone: true, Upstream: domain.UpstreamState{State: domain.UpstreamNone}}
	w.gitStates = &fakeGitStates{byTask: map[string][]domain.GitState{"t-feed": {aheadState("w1", w.sessions[0].ID, "agent/feed"), gone}}}
	rec := get(t, newTestHandler(t, w), "/projects/p1/tasks/t-feed")
	body := rec.Body.String()
	ssr := body[strings.Index(body, `<div x-ignore data-ssr role="group">`):]
	ssr = ssr[:strings.Index(ssr, `<template x-for="group in groups"`)]
	chips := strings.Count(ssr, `class="[ sync-state ]"`)
	if chips != 1 {
		t.Fatalf("the first paint has %d chips, want 1 (the gone worktree has none)", chips)
	}
	for _, want := range []string{`data-state="ahead"`, `<span class="[ upstream ]">origin</span>`, `<span class="[ visually-hidden ]"> ahead</span>`,
		`title="Upstream origin/agent/feed: 3 commits ahead, 0 commits behind`} {
		if !strings.Contains(ssr, want) {
			t.Errorf("the first paint lacks %q", want)
		}
	}
}

// The session history's head paints both comparisons and the fetch age from the
// state it read, in the span the browser hides once it holds the state.
func TestSessionHistoryHeadPaintsBothComparisons(t *testing.T) {
	w := historyWorld(t)
	w.gitStates = &fakeGitStates{byTask: map[string][]domain.GitState{"t-feed": {aheadState("ws-agent", "s-agent", "agent/add-sse-feed-1a2b")}}}
	body := get(t, newTestHandler(t, w), "/projects/p1/tasks/t-feed/sessions/s-agent/history").Body.String()
	for _, want := range []string{`vs <code>main</code>`, `data-state="diverged"`, `vs upstream`, `data-state="ahead"`, `<span class="upstream">origin</span>`, "not fetched yet"} {
		if !strings.Contains(body, want) {
			t.Errorf("the head lacks %q", want)
		}
	}
	if !strings.Contains(body, `"git_states":[{"workspace_id":"ws-agent"`) {
		t.Error("the head's seed holds no state for the browser to start from")
	}
}

// The Code tab's source bar paints the chip after the commit.
func TestCodeTabSourceBarPaintsTheChip(t *testing.T) {
	w := newCodeWorld(t, func(w *codeWorld, d *Deps) {
		withSession(domain.SessionRunning, func(w *codeWorld) string { return w.at.WorkspaceID })(w, d)
		st := aheadState(w.at.WorkspaceID, "s1", w.f.Branch)
		d.WorktreeGit = &fakeGitStates{byTask: map[string][]domain.GitState{"t1": {st}}}
	})
	resp, err := http.Get(w.srv.URL + "/projects/" + w.f.ProjectID + "/tasks/t1/sessions/s1/code/changes")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	body := string(raw)
	bar := body[strings.Index(body, `class="code-source-bar"`):]
	bar = bar[:strings.Index(bar, `</header>`)]
	if !strings.Contains(bar, `<span class="sync-slot">`) || !chipPattern.MatchString(bar) || !strings.Contains(bar, `<span class="upstream">origin</span>`) {
		t.Errorf("the source bar has no chip after the commit:\n%s", bar)
	}
	if strings.Index(bar, `class="commit"`) > strings.Index(bar, `class="sync-slot"`) {
		t.Error("the chip is not after the commit")
	}
}
