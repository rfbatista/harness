package claudetext

import (
	"context"
	"testing"
)

func TestModelAskerAsksTheModelItIsGivenWithTheSystemPromptAndInput(t *testing.T) {
	f := &fakeOneShot{result: claudeResult(`{"similarity":1}`)}
	got, err := NewModelAsker(f, "claude").Ask(context.Background(), "opus", "judge this", "the input")
	if err != nil || got != `{"similarity":1}` {
		t.Fatalf("got %q, %v", got, err)
	}
	if len(f.got) != 1 || f.got[0].Model != "opus" || f.got[0].System != "judge this" || f.got[0].Input != "the input" {
		t.Fatalf("request = %+v", f.got)
	}
}
