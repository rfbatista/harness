package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

func TestHTTPClient_DecodesListsAndSendsRequests(t *testing.T) {
	var gotStart ports.InteractiveRequest
	var gotEnd map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/list_projects", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"projects":[{"id":"p1","name":"api","root_dir":"/src/api"}]}`))
	})
	mux.HandleFunc("GET /api/sessions", func(w http.ResponseWriter, r *http.Request) {
		if q := r.URL.Query(); q.Get("ticket_id") != "t1" || q.Get("status") != "running,idle" {
			t.Errorf("sessions query = %v", q)
		}
		_, _ = w.Write([]byte(`{"sessions":[{"id":"s1","interactive":true,"claude_session_id":"c1","status":"running"}]}`))
	})
	mux.HandleFunc("POST /api/start_interactive_session", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotStart)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"session":{"id":"s2"},"launch":{"session_id":"s2","dir":"/wt","args":["--session-id","s2"]}}`))
	})
	mux.HandleFunc("POST /api/end_interactive_session", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotEnd)
		_, _ = w.Write([]byte(`{"session":{"id":"s2"}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := NewHTTPClient(srv.URL + "/")
	ctx := context.Background()

	ps, err := c.ListProjects(ctx)
	if err != nil || len(ps) != 1 || ps[0].RootDir != "/src/api" {
		t.Fatalf("ListProjects = %+v, %v", ps, err)
	}
	ss, err := c.ListSessions(ctx, SessionFilter{TicketID: "t1", Statuses: []domain.SessionStatus{domain.SessionRunning, domain.SessionIdle}})
	if err != nil || len(ss) != 1 || !ss[0].Interactive || ss[0].ClaudeSessionID != "c1" {
		t.Fatalf("ListSessions = %+v, %v", ss, err)
	}
	s, l, err := c.StartSession(ctx, ports.InteractiveRequest{ProjectID: "p1", TicketID: "t1", Prompt: "hi"})
	if err != nil || s.ID != "s2" || l.Dir != "/wt" || len(l.Args) != 2 {
		t.Fatalf("StartSession = %+v %+v, %v", s, l, err)
	}
	if gotStart.TicketID != "t1" || gotStart.Prompt != "hi" {
		t.Errorf("start body = %+v", gotStart)
	}
	if err := c.EndSession(ctx, "s2", 3, true); err != nil {
		t.Fatal(err)
	}
	if gotEnd["session_id"] != "s2" || gotEnd["exit_code"] != float64(3) || gotEnd["closed"] != true {
		t.Errorf("end body = %+v", gotEnd)
	}
}

func TestHTTPClient_ErrorsCarryTheCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"session is still running","code":"SESSION_ALREADY_RUNNING"}`))
	}))
	defer srv.Close()

	_, _, err := NewHTTPClient(srv.URL).ResumeSession(context.Background(), "s1")
	var api *APIError
	if !errors.As(err, &api) || api.Code != "SESSION_ALREADY_RUNNING" || api.Status != 409 {
		t.Fatalf("err = %#v, want APIError 409 SESSION_ALREADY_RUNNING", err)
	}
}

func TestHTTPClient_ServerDown(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	if err := NewHTTPClient(url).Health(context.Background()); err == nil {
		t.Fatal("Health against a closed server succeeded")
	}
}
