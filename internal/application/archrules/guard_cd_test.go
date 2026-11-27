package archrules

import (
	"context"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

func TestAWriteAfterACdTheHarnessCannotResolveIsRefusedBecauseItCouldBeProtected(t *testing.T) {
	w := newGuardWorld(t, domain.GuardConfig{ProtectedPaths: defaultProtected}, nil)
	check := func(unresolved ...string) ports.ArchitectureVerdict {
		return w.svc.CheckArchitecture(context.Background(), ports.ArchitectureCheck{
			SessionID: "s", Point: domain.PointBeforeTool, Tool: "Bash", Command: "cd $D && echo x > f", ShellUnresolved: unresolved})
	}
	v := check("f")
	if !strings.Contains(v.Deny, "PROTECTED_PATH") || !strings.Contains(v.Deny, "cd") {
		t.Fatalf("deny = %q", v.Deny)
	}
	if len(w.events.events) != 1 || w.events.events[0].RuleID != ProtectedPathRule || w.events.events[0].Outcome != domain.OutcomeBlocked {
		t.Errorf("events = %+v", w.events.events)
	}
	if v := check(); v.Deny != "" {
		t.Errorf("a command with nothing unresolved was refused: %q", v.Deny)
	}

	// With no protected path configured there is nothing it could be.
	none := newGuardWorld(t, domain.GuardConfig{}, nil)
	if v := none.svc.CheckArchitecture(context.Background(), ports.ArchitectureCheck{
		SessionID: "s", Point: domain.PointBeforeTool, Tool: "Bash", ShellUnresolved: []string{"f"}}); v.Deny != "" {
		t.Errorf("refused with no protected path: %q", v.Deny)
	}
	// A non-gated session is never refused.
	review := newGuardWorld(t, domain.GuardConfig{Mode: domain.SessionModeReview, ProtectedPaths: defaultProtected}, nil)
	if v := review.svc.CheckArchitecture(context.Background(), ports.ArchitectureCheck{
		SessionID: "s", Point: domain.PointBeforeTool, Tool: "Bash", ShellUnresolved: []string{"f"}}); v.Deny != "" {
		t.Errorf("a review session was refused: %q", v.Deny)
	}
}
