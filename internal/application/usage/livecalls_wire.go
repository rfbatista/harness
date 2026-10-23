package usage

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"encoding/json"
	"io"
	"strings"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The wire shapes of the live call stream (Contract: harness server ↔ web UI
// — live call stream) and the parsing of captured bytes into them.

type liveRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type liveRequest struct {
	Method  string              `json:"method"`
	Path    string              `json:"path"`
	Query   string              `json:"query"`
	Headers map[string][]string `json:"headers"`
	Body    json.RawMessage     `json:"body"`
}

type startedEventData struct {
	ID          string      `json:"id"`
	Attribution string      `json:"attribution"`
	ProjectID   *string     `json:"project_id"`
	Session     *liveRef    `json:"session"`
	Task        *liveRef    `json:"task"`
	Kind        string      `json:"kind"`
	StartedAt   string      `json:"started_at"`
	Request     liveRequest `json:"request"`
}

type liveUsage struct {
	InputTokens              int64    `json:"input_tokens"`
	OutputTokens             int64    `json:"output_tokens"`
	CacheCreationInputTokens int64    `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64    `json:"cache_read_input_tokens"`
	EstimatedCostUSD         *float64 `json:"estimated_cost_usd"`
}

type sseEvent struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

type liveResponse struct {
	Headers map[string][]string `json:"headers"`
	Body    json.RawMessage     `json:"body"`
	Events  []sseEvent          `json:"events"`
}

type finishedEventData struct {
	ID         string       `json:"id"`
	EndedAt    string       `json:"ended_at"`
	DurationMS int64        `json:"duration_ms"`
	Status     int          `json:"status"`
	Streamed   bool         `json:"streamed"`
	Model      string       `json:"model"`
	Error      string       `json:"error"`
	Usage      liveUsage    `json:"usage"`
	Response   liveResponse `json:"response"`
}

type missedEvent struct {
	Count int    `json:"count"`
	At    string `json:"at"`
}

func stamp(t time.Time) string { return t.Format("2006-01-02T15:04:05.000Z07:00") }

func startedEvent(r ports.CapturedRequest, who callOwner) startedEventData {
	ev := startedEventData{
		ID: r.ID, Attribution: r.Attribution, Task: who.task, Kind: who.kind, StartedAt: stamp(r.StartedAt),
		Request: liveRequest{Method: r.Method, Path: r.Path, Query: r.Query, Headers: redactHeaders(r.Header),
			Body: bodyValue(r.Body, r.Header.Get("Content-Encoding"))},
	}
	if who.projectID != "" {
		ev.ProjectID = &who.projectID
	}
	if who.session != nil {
		title := strings.TrimSpace(who.session.Task)
		if len(title) > sessionTitleLen {
			title = strings.ToValidUTF8(title[:sessionTitleLen], "") + "…"
		}
		if title == "" {
			title = who.session.ID
		}
		ev.Session = &liveRef{ID: who.session.ID, Title: title}
	}
	return ev
}

func finishedEvent(c ports.CapturedCall) finishedEventData {
	u := c.Usage
	rec := &domain.UsageRecord{Model: u.Model, InputTokens: u.InputTokens, OutputTokens: u.OutputTokens,
		CacheCreationInputTokens: u.CacheCreationInputTokens, CacheReadInputTokens: u.CacheReadInputTokens}
	ev := finishedEventData{
		ID: c.ID, EndedAt: stamp(u.EndedAt), DurationMS: u.EndedAt.Sub(u.StartedAt).Milliseconds(),
		Status: c.Status, Streamed: c.Streamed, Model: u.Model, Error: u.Error,
		Usage: liveUsage{InputTokens: u.InputTokens, OutputTokens: u.OutputTokens,
			CacheCreationInputTokens: u.CacheCreationInputTokens, CacheReadInputTokens: u.CacheReadInputTokens,
			EstimatedCostUSD: estimatedCost(rec)},
		Response: liveResponse{Headers: redactHeaders(c.Header)},
	}
	encoding := c.Header.Get("Content-Encoding")
	if body, ok := decodeBody(c.Body, encoding); c.Streamed && ok {
		ev.Response.Events = readEvents(body)
	} else {
		ev.Response.Body = bodyValue(c.Body, encoding)
	}
	return ev
}

// bodyValue is a captured body as a JSON value: its JSON when it parses, else
// a string of it as it is; null when it is empty.
func bodyValue(body []byte, encoding string) json.RawMessage {
	if len(body) == 0 {
		return nil
	}
	if decoded, ok := decodeBody(body, encoding); ok {
		return jsonOrString(decoded)
	}
	return asString(body)
}

func jsonOrString(b []byte) json.RawMessage {
	if len(b) == 0 {
		return nil
	}
	if json.Valid(b) {
		return json.RawMessage(bytes.TrimSpace(b))
	}
	return asString(b)
}

func asString(b []byte) json.RawMessage {
	s, _ := json.Marshal(string(b))
	return s
}

// decodeBody undoes a Content-Encoding header's encodings; false when one is
// unknown or the bytes do not decode.
func decodeBody(body []byte, encoding string) ([]byte, bool) {
	encodings := strings.Split(strings.ToLower(encoding), ",")
	for i := len(encodings) - 1; i >= 0; i-- {
		var open func(io.Reader) (io.Reader, error)
		switch strings.TrimSpace(encodings[i]) {
		case "", "identity":
			continue
		case "gzip", "x-gzip":
			open = func(r io.Reader) (io.Reader, error) { return gzip.NewReader(r) }
		case "deflate":
			open = func(r io.Reader) (io.Reader, error) {
				// HTTP's deflate is zlib-wrapped; some servers send it bare.
				if zr, err := zlib.NewReader(bytes.NewReader(body)); err == nil {
					return zr, nil
				}
				return flate.NewReader(r), nil
			}
		default:
			return nil, false
		}
		r, err := open(bytes.NewReader(body))
		if err != nil {
			return nil, false
		}
		out, err := io.ReadAll(r)
		if err != nil {
			return nil, false
		}
		body = out
	}
	return body, true
}

// readEvents splits a text/event-stream body into its events, in order, each
// data as JSON when it parses.
func readEvents(body []byte) []sseEvent {
	events := []sseEvent{}
	var name string
	var data []byte
	hasData := false
	flush := func() {
		if hasData {
			events = append(events, sseEvent{Event: name, Data: jsonOrString(data)})
		}
		name, data, hasData = "", nil, false
	}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSuffix(line, "\r")
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "event:"):
			name = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			if hasData {
				data = append(data, '\n')
			}
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " ")...)
			hasData = true
		}
	}
	flush()
	return events
}
