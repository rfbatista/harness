package eventbus

import (
	"context"
	"errors"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

func TestBus_DeliversInOrderToTypedHandlers(t *testing.T) {
	b := New()
	var got []string
	ports.On(b, func(_ context.Context, ev domain.PromptDeleted) error {
		got = append(got, "first:"+ev.PromptID)
		return nil
	})
	ports.On(b, func(_ context.Context, ev domain.PromptDeleted) error {
		got = append(got, "second:"+ev.PromptID)
		return nil
	})
	ports.On(b, func(_ context.Context, ev domain.AgentDeleted) error {
		got = append(got, "agent:"+ev.AgentID)
		return nil
	})

	if err := b.Publish(context.Background(), domain.PromptDeleted{PromptID: "p1"}, domain.AgentDeleted{AgentID: "a1"}); err != nil {
		t.Fatal(err)
	}
	if want := "first:p1,second:p1,agent:a1"; strings.Join(got, ",") != want {
		t.Fatalf("delivered %v, want %s", got, want)
	}
}

func TestBus_JoinsHandlerErrorsAndRunsEveryHandler(t *testing.T) {
	b := New()
	boom := errors.New("boom")
	ran := 0
	ports.On(b, func(context.Context, domain.ProjectDeleted) error { ran++; return boom })
	ports.On(b, func(context.Context, domain.ProjectDeleted) error { ran++; return nil })

	err := b.Publish(context.Background(), domain.ProjectDeleted{ProjectID: "p"})
	if !errors.Is(err, boom) || !strings.Contains(err.Error(), "project.deleted") {
		t.Fatalf("err = %v, want boom wrapped with the event name", err)
	}
	if ran != 2 {
		t.Fatalf("ran %d handlers, want 2: a failure must not stop the others", ran)
	}
}

func TestBus_NoSubscribersIsNotAnError(t *testing.T) {
	if err := New().Publish(context.Background(), domain.SkillDeleted{SkillID: "s"}); err != nil {
		t.Fatalf("publish with no subscribers = %v", err)
	}
}
