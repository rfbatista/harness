package ports

// Phases of the agent OAuth flow.
const (
	// AgentOAuthIdle: no flow runs and none has a result to show.
	AgentOAuthIdle = "idle"
	// AgentOAuthStarting: claude setup-token runs and has not printed the
	// authorization URL yet.
	AgentOAuthStarting = "starting"
	// AgentOAuthWaitingForCode: the URL is out and claude waits for the code.
	AgentOAuthWaitingForCode = "waiting_for_code"
	// AgentOAuthChecking: the code was handed to claude, which checks it.
	AgentOAuthChecking = "checking"
	// AgentOAuthDone: the token was captured and stored as the agent credential.
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
// `claude setup-token` (the managed claude, in the agent Claude home), hands
// over the authorization URL, takes the pasted code and stores the printed
// token as the agent credential (driving). One flow runs at a time; its state
// lives in the service, so a page that was left finds it again.
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
