package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/application/notifications"
	"operators-mcp/internal/application/notifications/notificationstest"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

func notificationsServer(t *testing.T) (*httptest.Server, ports.Notifier, string) {
	t.Helper()
	e := notificationstest.NewEnv(t)
	svc := e.Service(t, notifications.Options{})
	srv := httptest.NewServer(NewRouter(NewHandler(Services{Notifications: svc})))
	t.Cleanup(srv.Close)
	return srv, svc, e.ProjectA
}

func TestNotificationRoutesAnswerTheContractsStatusCodes(t *testing.T) {
	srv, svc, project := notificationsServer(t)
	n, err := svc.Notify(context.Background(), ports.NotifyRequest{ProjectID: project, Title: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		method, path, body string
		status             int
		code               string
	}{
		{"GET", "/api/notifications", "", 200, ""},
		{"GET", "/api/notifications?unread=true&limit=200&project_id=" + project, "", 200, ""},
		{"GET", "/api/notifications?unread=false", "", 200, ""},
		{"GET", "/api/notifications?unread=yes", "", 400, "INVALID_INPUT"},
		{"GET", "/api/notifications?limit=0", "", 400, "INVALID_INPUT"},
		{"GET", "/api/notifications?limit=201", "", 400, "INVALID_INPUT"},
		{"GET", "/api/notifications?limit=ten", "", 400, "INVALID_INPUT"},
		{"GET", "/api/notifications?before=nope", "", 400, "INVALID_INPUT"},
		{"POST", "/api/notifications/read", `{}`, 400, "INVALID_INPUT"},
		{"POST", "/api/notifications/read", `{"ids":["` + n.ID + `"],"all":true}`, 400, "INVALID_INPUT"},
		{"POST", "/api/notifications/read", `{"ids":["missing"]}`, 404, "NOTIFICATION_NOT_FOUND"},
		{"POST", "/api/notifications/read", `{"ids":["` + n.ID + `"]}`, 200, ""},
		{"POST", "/api/notifications/read", `{"all":true,"project_id":"` + project + `"}`, 200, ""},
	}
	for _, c := range cases {
		req, _ := http.NewRequest(c.method, srv.URL+c.path, strings.NewReader(c.body))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if resp.StatusCode != c.status || (c.code != "" && body["code"] != c.code) {
			t.Errorf("%s %s %s = %d %v, want %d %s", c.method, c.path, c.body, resp.StatusCode, body, c.status, c.code)
		}
		if c.status == 200 {
			if _, ok := body["unread_count"]; !ok {
				t.Errorf("%s %s answered %v, want unread_count", c.method, c.path, body)
			}
			if list, ok := body["notifications"].([]any); !ok || list == nil {
				t.Errorf("%s %s answered %v, want a notifications list", c.method, c.path, body)
			}
		}
	}
}

func TestTheRateLimitIs429(t *testing.T) {
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/", nil), rec)
	errorHandler(&domain.StructuredError{Code: "NOTIFICATION_RATE_LIMITED", Message: "slow down"}, c)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("NOTIFICATION_RATE_LIMITED = %d, want 429", rec.Code)
	}
}

func TestNotificationsUnconfiguredAnswer503(t *testing.T) {
	srv := httptest.NewServer(NewRouter(NewHandler(Services{})))
	defer srv.Close()
	for _, path := range []string{"/api/notifications", "/api/notifications/events"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("GET %s with no notifications = %d, want 503", path, resp.StatusCode)
		}
	}
}

func TestTheNotificationStreamIsBareDataLines(t *testing.T) {
	srv, svc, project := notificationsServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/notifications/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q", ct)
	}
	n, err := svc.Notify(context.Background(), ports.NotifyRequest{ProjectID: project, Title: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(resp.Body)
	var lines []string
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("stream ended after %q: %v", lines, err)
		}
		line = strings.TrimRight(line, "\n")
		if line == "" && len(lines) > 0 {
			break
		}
		lines = append(lines, line)
	}
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "data: ") {
		t.Fatalf("event = %q, want exactly one data: line and no event: name", lines)
	}
	var change struct {
		Changed     []map[string]any `json:"changed"`
		UnreadCount int              `json:"unread_count"`
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(lines[0], "data: ")), &change); err != nil {
		t.Fatal(err)
	}
	if len(change.Changed) != 1 || change.Changed[0]["id"] != n.ID || change.Changed[0]["project_name"] != "Alpha" || change.UnreadCount != 1 {
		t.Fatalf("change = %+v", change)
	}
}
