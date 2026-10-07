package ports

import (
	"encoding/json"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

// Each architect-channel change routes to its project and travels alone on
// the wire, under its own key.
func TestProjectChangeArchitectChannelKeys(t *testing.T) {
	cases := map[string]ProjectChange{
		"task_message":   {TaskMessage: &domain.TaskMessage{ID: "m", ProjectID: "p"}},
		"review_request": {ReviewRequest: &domain.ReviewRequest{ID: "r", ProjectID: "p"}},
		"status_check":   {StatusCheck: &StatusCheckEvent{StatusCheck: domain.StatusCheck{DelegateSessionID: "d", ProjectID: "p"}}},
		"task_status":    {TaskStatus: &domain.TaskStatusChange{TaskID: "t", ProjectID: "p", By: domain.ChangedByPerson}},
	}
	for key, c := range cases {
		if c.ProjectID() != "p" {
			t.Errorf("%s: ProjectID %q", key, c.ProjectID())
		}
		b, _ := json.Marshal(c)
		var keys map[string]json.RawMessage
		_ = json.Unmarshal(b, &keys)
		if len(keys) != 1 || keys[key] == nil {
			t.Errorf("%s: want only that key, got %s", key, b)
		}
		if strings.Contains(string(b), `"project_id":"p"`) && key != "review_request" {
			t.Errorf("%s: project id is bookkeeping, not wire: %s", key, b)
		}
	}
}
