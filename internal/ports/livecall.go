package ports

import (
	"context"
	"net/http"
	"time"
)

// The live call log (see "Spec: harness — live call log"): the LLM proxy
// hands each call it serves to a CallCapture while a live call viewer is
// connected, and the capture fans it out to the viewers. Nothing is stored.
// CapturedRequest, CapturedCall and CallCapture have the shape of spillway's
// llmproxy types of the same names (Contract: harness ↔ spillway — call
// capture), so the proxy adapter converts one to the other field by field.

// RedactedValue replaces the value of every credential header on the live
// call stream.
const RedactedValue = "[redacted]"

// CapturedRequest is a request the proxy is about to forward upstream.
type CapturedRequest struct {
	// ID is the same ID as the request's usage record.
	ID string
	// Attribution is the attribution header's value, "" if absent.
	Attribution string
	Method      string
	// Path is the upstream path, e.g. "/v1/messages".
	Path string
	// Query is the raw query, without "?".
	Query string
	// Header is as sent upstream. The capture redacts credential values itself
	// too, so a header that arrives unredacted never reaches a viewer.
	Header http.Header
	// Body is the full request body as received, possibly compressed.
	Body      []byte
	StartedAt time.Time
}

// CapturedCall is a captured request's end.
type CapturedCall struct {
	ID string
	// Status is the upstream HTTP status; 0 if upstream was never reached.
	Status int
	Header http.Header
	// Body is the full response body as the client received it, possibly
	// compressed: JSON, or the raw text/event-stream bytes.
	Body     []byte
	Streamed bool
	// Usage is the usage record the UsageSink receives for the same call.
	Usage UsageRequest
}

// CallCapture receives copies of a request and its response while a live
// call viewer is connected. It never blocks, and never fails.
type CallCapture interface {
	// Enabled is true exactly while at least one live call viewer is
	// connected, on any project. The proxy asks it once per request.
	Enabled() bool
	// Started is called once per captured request, before it is forwarded.
	Started(ctx context.Context, r CapturedRequest)
	// Finished is called once per captured request, after its response ended.
	Finished(ctx context.Context, c CapturedCall)
}

// Names of the live call stream's events.
const (
	LiveCallReady    = "ready"
	LiveCallStarted  = "call.started"
	LiveCallFinished = "call.finished"
	LiveCallsMissed  = "calls.missed"
)

// LiveCallEvent is one event of the live call stream; Data is its JSON value.
type LiveCallEvent struct {
	Name string
	Data any
}

// LiveCalls opens live call viewers (Contract: harness server ↔ web UI —
// live call stream).
type LiveCalls interface {
	// OpenLiveCalls makes the caller a live call viewer of a project: capture
	// is on from now until the viewer is closed. It sees the calls that
	// resolve to the project and those that resolve to none, starting from
	// now.
	OpenLiveCalls(projectID string) LiveCallViewer
}

// LiveCallViewer is one open live call stream's queue of events. Its buffer
// is bounded: when it is full the events that do not fit are discarded, and a
// calls.missed event with their count takes their place. The viewer stays open.
type LiveCallViewer interface {
	// Ready receives a value whenever Take may have something.
	Ready() <-chan struct{}
	// Take removes the next event; false when there is none now.
	Take() (LiveCallEvent, bool)
	// Close ends the viewer and keeps nothing of it.
	Close()
}
