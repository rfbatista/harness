package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// updatedAt reads the view's updated_at: nil when the level was never saved.
func updatedAt(t *testing.T, body []byte) *time.Time {
	t.Helper()
	var view struct {
		UpdatedAt *time.Time `json:"updated_at"`
	}
	if err := json.Unmarshal(body, &view); err != nil {
		t.Fatal(err)
	}
	return view.UpdatedAt
}

func TestAViewAnswersWhenItsLevelWasLastSavedAndNullWhenNeverSaved(t *testing.T) {
	h := agentSystemHandler(t)
	for _, c := range []struct{ name, path string }{
		{"project", "/projects/p1/agent-system"},
		{"repository", "/projects/p1/repositories/r1/agent-system"},
	} {
		t.Run(c.name, func(t *testing.T) {
			rec := getJSON(t, h, c.path)
			if rec.Code != http.StatusOK || !json.Valid(rec.Body.Bytes()) {
				t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
			}
			var raw map[string]json.RawMessage
			_ = json.Unmarshal(rec.Body.Bytes(), &raw)
			if got, ok := raw["updated_at"]; !ok || string(got) != "null" {
				t.Fatalf("updated_at before any save = %s (present %v), want null", got, ok)
			}

			before := time.Now().Add(-time.Second)
			rec = patchJSON(t, h, c.path, `{"guard.retry_limit": 7}`)
			saved := updatedAt(t, rec.Body.Bytes())
			if saved == nil || saved.Before(before) {
				t.Fatalf("updated_at after the save = %v, want just now: %s", saved, rec.Body.String())
			}
			if again := updatedAt(t, getJSON(t, h, c.path).Body.Bytes()); again == nil || !again.Equal(*saved) {
				t.Fatalf("GET after the save = %v, want %v", again, saved)
			}

			// A save that only resets still moves it: it is the last save, not the newest row.
			time.Sleep(15 * time.Millisecond)
			rec = patchJSON(t, h, c.path, `{"guard.retry_limit": null}`)
			reset := updatedAt(t, rec.Body.Bytes())
			if reset == nil || !reset.After(*saved) {
				t.Fatalf("updated_at after a reset = %v, want after %v", reset, saved)
			}
		})
	}
}

func TestARefusedSaveDoesNotMoveTheLastSaved(t *testing.T) {
	h := agentSystemHandler(t)
	patchJSON(t, h, "/projects/p1/agent-system", `{"guard.retry_limit": 7}`)
	first := updatedAt(t, getJSON(t, h, "/projects/p1/agent-system").Body.Bytes())
	if rec := patchJSON(t, h, "/projects/p1/agent-system", `{"guard.retry_limit": 99}`); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if got := updatedAt(t, getJSON(t, h, "/projects/p1/agent-system").Body.Bytes()); first == nil || got == nil || !got.Equal(*first) {
		t.Fatalf("after a refused save = %v, want %v", got, first)
	}
}
