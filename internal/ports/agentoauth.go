package ports

import "context"

// Phases of the agent OAuth flow.
const (
	// AgentOAuthIdle: no flow runs and none has a result to show.
	AgentOAuthIdle = "idle"
	// AgentOAuthStarting: claude auth login runs and has not printed the
	// authorization URL yet.
	AgentOAuthStarting = "starting"
	// AgentOAuthWaitingForCode: the URL is out and claude waits for the code.
	AgentOAuthWaitingForCode = "waiting_for_code"
	// AgentOAuthChecking: the code was handed to claude, which checks it;
	// once it exits cleanly, `claude auth status` decides.
	AgentOAuthChecking = "checking"
	// AgentOAuthDone: claude exited cleanly and reports a signed-in account;
	// it keeps the login in the agent Claude home, the harness stores none.
	AgentOAuthDone = "done"
	// AgentOAuthFailed: the flow ended without saving anything; Message says why.
	AgentOAuthFailed = "failed"
)

// AgentOAuthState is what the person sees of the flow. It never carries the
// token, nor the code the person pasted.
type AgentOAuthState struct {
	Phase string `json:"phase"`
	// URL is the authorization page to open; set from WaitingForCode on.
	URL string `json:"url"`
	// RemainingSeconds is what is left before the flow times out; 0 when no
	// flow runs.
	RemainingSeconds int `json:"remaining_seconds"`
	// Message says why a flow failed, in the person's words.
	Message string `json:"message"`
	// Detail is the last line the process printed when it died on its own.
	Detail string `json:"detail"`
}

// AgentOAuth signs agents in with the person's Claude subscription: it runs
// `claude auth login --claudeai` (the managed claude, in the agent Claude
// home, under the agent sandbox where the platform has one), hands over the
// authorization URL and takes the pasted code (driving). claude keeps the
// login in the agent home; the harness stores no token. One flow runs at a
// time; its state lives in the service, so a page that was left finds it
// again.
type AgentOAuth interface {
	// State is the current flow, or the result of the last one.
	State() AgentOAuthState
	// Start begins a flow. Refuses AGENT_OAUTH_BUSY while one runs and
	// CLAUDE_CLI_NOT_FOUND when there is no managed claude to run.
	Start() (AgentOAuthState, error)
	// Submit hands the code to the waiting process. Refuses INVALID_INPUT for
	// an empty code and AGENT_OAUTH_NOT_WAITING when no flow waits for one.
	Submit(code string) (AgentOAuthState, error)
	// Cancel ends the flow, kills the process and clears the state. Nothing
	// is saved or removed.
	Cancel() AgentOAuthState
}

// AgentAuthStatus is what `claude auth status` reports about the agent
// Claude home's login. It carries no token.
type AgentAuthStatus struct {
	LoggedIn         bool   `json:"logged_in"`
	AuthMethod       string `json:"auth_method"`
	SubscriptionType string `json:"subscription_type"`
	Email            string `json:"email"`
}

// AgentAuth asks the managed claude who the agent Claude home is signed in
// as (driven), run the way the login runs: in that home, under the sandbox
// driver where the platform has one.
type AgentAuth interface {
	// Status runs `claude auth status` and answers what it reports. A claude
	// that cannot run, or whose answer cannot be read, is an error.
	Status(ctx context.Context) (AgentAuthStatus, error)
}
