package orchestration

import "testing"

func TestAnEndedSessionsActivationsAreFreed(t *testing.T) {
	var a activatedOnTouch
	if !a.first("s1", "go") || a.first("s1", "go") {
		t.Fatal("a skill loads once per session")
	}
	a.forget("s1")
	if len(a.done) != 0 {
		t.Errorf("the ended session is still held: %v", a.done)
	}
}
