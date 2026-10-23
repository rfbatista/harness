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
}

func (f *fakeMerger) MergeSessionBranch(_ context.Context, id string) (*domain.MergeResult, error) {
	f.got = id
	return f.res, f.err
}

func (f *fakeMerger) MergeOffer(_ context.Context, id string) (*ports.MergeOffer, error) {
	f.got = id
	return f.offer, f.err
}

func TestHTTP_SessionMerge_AnswersTheResult(t *testing.T) {
	m := &fakeMerger{res: &domain.MergeResult{At: time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC), Merged: true, Into: "main", Head: "abc"}}
	code, body := doJSON(t, NewHandler(Services{Merges: m}), http.MethodPost, "/api/sessions/s1/merge", "")
	if code != http.StatusOK || m.got != "s1" || body["merged"] != true || body["into"] != "main" || body["head"] != "abc" {
		t.Fatalf("%d asked %q: %v", code, m.got, body)
	}
}

func TestHTTP_SessionMerge_EachRefusalIsCodedAtItsStatus(t *testing.T) {
	for code, status := range map[string]int{
		domain.MergeNoBranch: 409, domain.MergeNoCheckout: 409, domain.MergeCheckoutDirty: 409, domain.MergeWrongBranch: 409,
		domain.MergeAlreadyMerged: 409, domain.MergeConflict: 409, domain.MergeBranchNotFound: 409,
		domain.MergeFailed: 500, "SESSION_NOT_FOUND": 404,
	} {
		m := &fakeMerger{err: &domain.StructuredError{Code: code, Message: "because " + code}}
		got, body := doJSON(t, NewHandler(Services{Merges: m}), http.MethodPost, "/api/sessions/s1/merge", "")
		if got != status || body["code"] != code || body["error"] != "because "+code {
			t.Errorf("%s: %d %v, want %d", code, got, body, status)
		}
	}
}

func TestHTTP_SessionMergeOffer(t *testing.T) {
	m := &fakeMerger{offer: &ports.MergeOffer{Available: true, Into: "main"}}
	code, body := doJSON(t, NewHandler(Services{Merges: m}), http.MethodGet, "/api/sessions/s1/merge", "")
	if code != http.StatusOK || body["available"] != true || body["into"] != "main" {
		t.Fatalf("%d %v", code, body)
	}
	if code, _ := doJSON(t, NewHandler(Services{}), http.MethodGet, "/api/sessions/s1/merge", ""); code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured = %d", code)
	}
}
