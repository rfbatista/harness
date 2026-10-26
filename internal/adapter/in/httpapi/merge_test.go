package httpapi

import (
	"context"
	"net/http"
	"testing"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

type fakeMerger struct {
	res   *domain.MergeResult
	offer *ports.MergeOffer
	err   error
	got   string
	req   ports.MergeRequest
	asked string // the target the offer was asked about
}

func (f *fakeMerger) MergeSessionBranch(_ context.Context, id string) (*domain.MergeResult, error) {
	f.got = id
	return f.res, f.err
}

func (f *fakeMerger) MergeInto(_ context.Context, id string, req ports.MergeRequest) (*domain.MergeResult, error) {
	f.got, f.req = id, req
	return f.res, f.err
}

func (f *fakeMerger) MergeOffer(_ context.Context, id, target string) (*ports.MergeOffer, error) {
	f.got, f.asked = id, target
	return f.offer, f.err
}

func TestHTTP_SessionMerge_AnswersTheResult(t *testing.T) {
	m := &fakeMerger{res: &domain.MergeResult{At: time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC), Merged: true, Into: "main", Method: domain.MergeMethodMerge, Head: "abc"}}
	code, body := doJSON(t, NewHandler(Services{Merges: m}), http.MethodPost, "/api/sessions/s1/merge", "")
	if code != http.StatusOK || m.got != "s1" || body["merged"] != true || body["into"] != "main" || body["head"] != "abc" || body["method"] != "merge" {
		t.Fatalf("%d asked %q: %v", code, m.got, body)
	}
	if m.req != (ports.MergeRequest{}) {
		t.Fatalf("an empty body asked %+v, want the zero request (default target, merge commit)", m.req)
	}
}

func TestHTTP_SessionMerge_PassesTheTargetMethodAndMessage(t *testing.T) {
	m := &fakeMerger{res: &domain.MergeResult{Merged: true, Into: "release", Method: domain.MergeMethodSquash, Head: "h"}}
	code, body := doJSON(t, NewHandler(Services{Merges: m}), http.MethodPost, "/api/sessions/s1/merge",
		`{"target":"release","method":"squash","message":"Ship it"}`)
	if code != http.StatusOK || body["method"] != "squash" || body["into"] != "release" {
		t.Fatalf("%d %v", code, body)
	}
	if want := (ports.MergeRequest{Target: "release", Method: domain.MergeMethodSquash, Message: "Ship it"}); m.req != want {
		t.Fatalf("asked %+v, want %+v", m.req, want)
	}
	if code, _ := doJSON(t, NewHandler(Services{Merges: m}), http.MethodPost, "/api/sessions/s1/merge", `{"target":`); code != http.StatusBadRequest {
		t.Fatalf("a broken body = %d", code)
	}
}

func TestHTTP_SessionMerge_EachRefusalIsCodedAtItsStatus(t *testing.T) {
	for code, status := range map[string]int{
		domain.MergeNoBranch: 409, domain.MergeNoCheckout: 409, domain.MergeCheckoutDirty: 409, domain.MergeWrongBranch: 409,
		domain.MergeAlreadyMerged: 409, domain.MergeConflict: 409, domain.MergeBranchNotFound: 409, domain.MergeTargetNotFound: 409,
		domain.MergeFailed: 500, "SESSION_NOT_FOUND": 404, "INVALID_INPUT": 400,
	} {
		m := &fakeMerger{err: &domain.StructuredError{Code: code, Message: "because " + code}}
		got, body := doJSON(t, NewHandler(Services{Merges: m}), http.MethodPost, "/api/sessions/s1/merge", "")
		if got != status || body["code"] != code || body["error"] != "because "+code {
			t.Errorf("%s: %d %v, want %d", code, got, body, status)
		}
	}
}

func TestHTTP_SessionMerge_AConflictNamesItsFiles(t *testing.T) {
	m := &fakeMerger{err: &domain.DetailedError{
		StructuredError: &domain.StructuredError{Code: domain.MergeConflict, Message: "conflicts in a.txt"},
		Details:         map[string]any{"files": []string{"a.txt"}},
	}}
	code, body := doJSON(t, NewHandler(Services{Merges: m}), http.MethodPost, "/api/sessions/s1/merge", "")
	files, _ := body["details"].(map[string]any)["files"].([]any)
	if code != 409 || len(files) != 1 || files[0] != "a.txt" {
		t.Fatalf("%d %v", code, body)
	}
}

func TestHTTP_SessionMergeOffer(t *testing.T) {
	m := &fakeMerger{offer: &ports.MergeOffer{
		Available: true, Branch: "agent/x", Into: "release", Ahead: 3, UncommittedChanges: 2, SuggestedMessage: "Ship it",
		Targets: []ports.MergeTarget{{Name: "main", Default: true, CheckedOut: true, Dirty: true}, {Name: "release"}},
	}}
	code, body := doJSON(t, NewHandler(Services{Merges: m}), http.MethodGet, "/api/sessions/s1/merge?target=release", "")
	if code != http.StatusOK || m.got != "s1" || m.asked != "release" {
		t.Fatalf("%d asked %q about %q: %v", code, m.got, m.asked, body)
	}
	for k, v := range map[string]any{"available": true, "branch": "agent/x", "into": "release", "reason": "", "ahead": float64(3),
		"uncommitted_changes": float64(2), "suggested_message": "Ship it"} {
		if body[k] != v {
			t.Errorf("%s = %v (%T), want %v", k, body[k], body[k], v)
		}
	}
	targets, _ := body["targets"].([]any)
	first, _ := targets[0].(map[string]any)
	if len(targets) != 2 || first["name"] != "main" || first["default"] != true || first["checked_out"] != true || first["dirty"] != true {
		t.Fatalf("targets = %v", body["targets"])
	}

	if _, _ = doJSON(t, NewHandler(Services{Merges: m}), http.MethodGet, "/api/sessions/s1/merge", ""); m.asked != "" {
		t.Fatalf("no target asked %q, want the default", m.asked)
	}
	if code, _ := doJSON(t, NewHandler(Services{}), http.MethodGet, "/api/sessions/s1/merge", ""); code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured = %d", code)
	}
}

func (f *fakeMerger) BranchChangedSince(context.Context, string, time.Time) (bool, error) {
	return false, nil
}
