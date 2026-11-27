package domain

// The critic (Glossary "Critic"): the Reviewer the harness launches on its own
// after a green gate run of a gated session. Advisory, one pass per diff.

// CriticLaunchInput is what decides whether a gate run launches the critic.
type CriticLaunchInput struct {
	// Enabled is the effective review.critic.enabled.
	Enabled bool
	// Gated says the author session is gated.
	Gated bool
	// Phase is the gate run change's phase: "started" or "finished".
	Phase   string
	Result  GateRunResult
	Trigger GateTrigger
	// DiffSeen says a critic already reviewed this base and head.
	DiffSeen bool
}

// CriticSkip is why a gate run does not launch the critic; CriticLaunches
// (empty) says it does.
type CriticSkip string

const (
	CriticLaunches        CriticSkip = ""
	CriticSkipDisabled    CriticSkip = "critic_disabled"
	CriticSkipNotGated    CriticSkip = "session_not_gated"
	CriticSkipNotFinished CriticSkip = "run_not_finished"
	CriticSkipPractice    CriticSkip = "practice_run"
	CriticSkipNotGreen    CriticSkip = "run_not_green"
	CriticSkipDiffSeen    CriticSkip = "diff_already_reviewed"
)

// DecideCritic is the launch decision table: the critic launches after a
// finished, green run whose trigger is the done declaration or the person,
// when it is on, the session is gated and its diff was not reviewed before.
func DecideCritic(in CriticLaunchInput) CriticSkip {
	switch {
	case in.Phase != "finished":
		return CriticSkipNotFinished
	case !in.Enabled:
		return CriticSkipDisabled
	case !in.Gated:
		return CriticSkipNotGated
	case in.Trigger != GateTriggerDeclaration && in.Trigger != GateTriggerPerson:
		return CriticSkipPractice
	case in.Result != GateRunGreen:
		return CriticSkipNotGreen
	case in.DiffSeen:
		return CriticSkipDiffSeen
	}
	return CriticLaunches
}
