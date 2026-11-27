package domain

import "testing"

func TestTheCriticLaunchDecision(t *testing.T) {
	ok := CriticLaunchInput{Enabled: true, Gated: true, Phase: "finished", Result: GateRunGreen, Trigger: GateTriggerDeclaration}
	with := func(f func(*CriticLaunchInput)) CriticLaunchInput { in := ok; f(&in); return in }
	cases := []struct {
		name string
		in   CriticLaunchInput
		want CriticSkip
	}{
		{"green declaration run", ok, CriticLaunches},
		{"green run the person triggered", with(func(i *CriticLaunchInput) { i.Trigger = GateTriggerPerson }), CriticLaunches},
		{"practice run", with(func(i *CriticLaunchInput) { i.Trigger = GateTriggerPractice }), CriticSkipPractice},
		{"critic off", with(func(i *CriticLaunchInput) { i.Enabled = false }), CriticSkipDisabled},
		{"session not gated", with(func(i *CriticLaunchInput) { i.Gated = false }), CriticSkipNotGated},
		{"run only started", with(func(i *CriticLaunchInput) { i.Phase = "started" }), CriticSkipNotFinished},
		{"red run", with(func(i *CriticLaunchInput) { i.Result = GateRunRed }), CriticSkipNotGreen},
		{"not passed run", with(func(i *CriticLaunchInput) { i.Result = GateRunNotPassed }), CriticSkipNotGreen},
		{"diff reviewed before", with(func(i *CriticLaunchInput) { i.DiffSeen = true }), CriticSkipDiffSeen},
	}
	for _, c := range cases {
		if got := DecideCritic(c.in); got != c.want {
			t.Errorf("%s: DecideCritic = %q, want %q", c.name, got, c.want)
		}
	}
}

// When several reasons hold at once the table names the first: a run that has
// not finished is never judged on its result, a practice run is a practice run
// even when red, and a red run is red even when its diff was seen.
func TestTheCriticLaunchDecisionNamesTheFirstReasonWhenSeveralHold(t *testing.T) {
	cases := []struct {
		name string
		in   CriticLaunchInput
		want CriticSkip
	}{
		{"unfinished beats disabled", CriticLaunchInput{Phase: "started"}, CriticSkipNotFinished},
		{"disabled beats not gated", CriticLaunchInput{Phase: "finished", Trigger: GateTriggerDeclaration, Result: GateRunGreen}, CriticSkipDisabled},
		{"not gated beats practice", CriticLaunchInput{Phase: "finished", Enabled: true, Trigger: GateTriggerPractice, Result: GateRunGreen}, CriticSkipNotGated},
		{"practice beats red", CriticLaunchInput{Phase: "finished", Enabled: true, Gated: true, Trigger: GateTriggerPractice, Result: GateRunRed}, CriticSkipPractice},
		{"red beats diff seen", CriticLaunchInput{Phase: "finished", Enabled: true, Gated: true, Trigger: GateTriggerPerson, Result: GateRunRed, DiffSeen: true}, CriticSkipNotGreen},
		{"empty trigger is no declaration", CriticLaunchInput{Phase: "finished", Enabled: true, Gated: true, Result: GateRunGreen}, CriticSkipPractice},
		{"empty result is not green", CriticLaunchInput{Phase: "finished", Enabled: true, Gated: true, Trigger: GateTriggerDeclaration}, CriticSkipNotGreen},
	}
	for _, c := range cases {
		if got := DecideCritic(c.in); got != c.want {
			t.Errorf("%s: DecideCritic = %q, want %q", c.name, got, c.want)
		}
	}
}
