package domain

import (
	"fmt"
	"strings"
)

// A service request is an agent's ask to change a service: create it, update
// its definition, or start, restart or stop it. The person answers it on the
// Services page or in the inbox, and on approval the harness does exactly that
// action as the agent's session (ADR 0083; contract "Harness server ↔ Web UI —
// services" §13).

// ServiceRequestAction is what the request asks for.
type ServiceRequestAction string

const (
	ServiceRequestCreate  ServiceRequestAction = "create"
	ServiceRequestUpdate  ServiceRequestAction = "update"
	ServiceRequestStart   ServiceRequestAction = "start"
	ServiceRequestRestart ServiceRequestAction = "restart"
	ServiceRequestStop    ServiceRequestAction = "stop"
)

// ServiceRequestState is where a request stands: pending until the person
// answers; approved, denied or expired; then, once an approved action ran, done
// or failed.
type ServiceRequestState string

const (
	ServiceRequestPending  ServiceRequestState = "pending"
	ServiceRequestApproved ServiceRequestState = "approved"
	ServiceRequestDenied   ServiceRequestState = "denied"
	// ServiceRequestExpired means the session ended while it was pending.
	ServiceRequestExpired ServiceRequestState = "expired"
	ServiceRequestDone    ServiceRequestState = "done"
	ServiceRequestFailed  ServiceRequestState = "failed"
)

// Open says the request still waits on the person.
func (s ServiceRequestState) Open() bool { return s == ServiceRequestPending }

// ServiceRequestChange is one field an update changes, with its readable
// before and after. A variable's values are never in it.
type ServiceRequestChange struct {
	Field  string `json:"field"`
	Before string `json:"before"`
	After  string `json:"after"`
}

// ServiceRequestEnv is one variable a request sets or unsets, or, for a start
// or restart, one run override. Value is left out of what a feed carries.
type ServiceRequestEnv struct {
	Name        string `json:"name"`
	Value       string `json:"value,omitempty"`
	Change      string `json:"change"` // set | unset
	PreviousSet bool   `json:"previous_set"`
}

// ServiceRequest is one request, as the person reads it.
type ServiceRequest struct {
	ID          string               `json:"id"`
	ProjectID   string               `json:"project_id"`
	ServiceID   string               `json:"service_id"`
	ServiceName string               `json:"service_name"`
	Action      ServiceRequestAction `json:"action"`
	SessionID   string               `json:"session_id"`
	TaskID      string               `json:"task_id"`
	Agent       string               `json:"agent"`
	Reason      string               `json:"reason"`
	Summary     string               `json:"summary"`
	// Proposed is the whole definition (create) or the one after the change
	// (update); nil for the other actions.
	Proposed *ServiceInput          `json:"proposed"`
	Changes  []ServiceRequestChange `json:"changes"`
	Env      []ServiceRequestEnv    `json:"env"`
	// Worktree is main or own for a start or restart, "" for the others.
	Worktree string `json:"worktree"`
	// OverridesGiven says a start or restart named its run overrides, even none
	// (a restart that did not repeats the last run's).
	OverridesGiven bool                `json:"overrides_given"`
	State          ServiceRequestState `json:"state"`
	Note           string              `json:"note"`
	ResultError    string              `json:"result_error"`
	RequestedAt    string              `json:"requested_at"`
	AnsweredAt     string              `json:"answered_at"`
}

// WithoutValues is a copy that holds no variable value: what a feed or an
// inbox notification may carry.
func (r ServiceRequest) WithoutValues() ServiceRequest {
	c := r
	c.Env = make([]ServiceRequestEnv, len(r.Env))
	for i, e := range r.Env {
		e.Value = ""
		c.Env[i] = e
	}
	c.Changes = append([]ServiceRequestChange{}, r.Changes...)
	if r.Proposed != nil {
		p := *r.Proposed
		p.Env = ""
		c.Proposed = &p
	}
	return c
}

// ServiceRequestFilter picks the requests a list answers.
type ServiceRequestFilter struct {
	ProjectID string
	ServiceID string
	SessionID string
	State     ServiceRequestState
}

// Codes of a service request.
const (
	// ServiceRequestNotFound: no request has that id.
	ServiceRequestNotFound = "SERVICE_REQUEST_NOT_FOUND"
	// RequestNotPending: the request is already answered, expired or done.
	RequestNotPending = "REQUEST_NOT_PENDING"
	// ServiceRequestDenied and ServiceRequestExpiredCode end an agent's blocked call.
	ServiceRequestDeniedCode  = "SERVICE_REQUEST_DENIED"
	ServiceRequestExpiredCode = "SERVICE_REQUEST_EXPIRED"
)

// DeniedMessage is what an agent's call fails with when the person denies.
func (r ServiceRequest) DeniedMessage() string {
	if note := strings.TrimSpace(r.Note); note != "" {
		return "the person denied this service request: " + note
	}
	return "the person denied this service request"
}

// ServiceRequestChanged: a service request was filed, answered, performed or
// expired; Request is it as it is now, with its values.
type ServiceRequestChanged struct {
	Request   ServiceRequest
	ProjectID string
}

func (ServiceRequestChanged) EventName() string { return "service_request.changed" }

// String names a request for a log line; it holds no value.
func (r ServiceRequest) String() string {
	return fmt.Sprintf("service request %s (%s %s)", r.ID, r.Action, r.ServiceName)
}
