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
