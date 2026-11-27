package archrules

import (
	"context"
	"errors"
	"testing"

	"operators-mcp/internal/domain"
)

type brokenGuard struct{ kept bool }

func (b brokenGuard) Keep(context.Context, string, domain.GuardConfig) error { return nil }
func (b brokenGuard) Config(context.Context, string) (domain.GuardConfig, bool, error) {
	if b.kept {
		return domain.GuardConfig{}, false, nil // the session kept none
	}
	return domain.GuardConfig{}, false, errors.New("the store is down")
}
func (b brokenGuard) CountRed(context.Context, string) (int, error) { return 0, nil }
func (b brokenGuard) MarkEscalated(context.Context, string) error   { return nil }
func (b brokenGuard) ResumeRed(context.Context, string) error       { return nil }
func (b brokenGuard) ResetRed(context.Context, string) error        { return nil }

func TestAConfigurationThatCannotBeReadOrWasNeverKeptHoldsAGatedSessionToTheHarnessDefaults(t *testing.T) {
	for name, g := range map[string]brokenGuard{"the store fails": {}, "none was kept": {kept: true}} {
		w := newGuardWorld(t, domain.GuardConfig{}, func(d *Deps) { d.Guard = g })
		if v := w.edit(w.write("check.sh", "ok\n")); v.Deny == "" {
			t.Errorf("%s: a write of check.sh by an implement session was not refused", name)
		}
		if len(w.events.events) != 1 {
			t.Errorf("%s: events = %+v", name, w.events.events)
		}
	}
	// A mode that is not gated stays free whatever the store says.
	w := newGuardWorld(t, domain.GuardConfig{}, func(d *Deps) {
		d.Guard = brokenGuard{}
		d.Sessions = sessionsByID{"s": {ID: "s", RepositoryID: "r", ProjectID: "p", WorkingDir: "", Mode: domain.SessionModeReview, Access: domain.AccessWrite}}
	})
	if v := w.edit("/nowhere/check.sh"); v.Deny != "" {
		t.Errorf("a review session was refused: %q", v.Deny)
	}
}
