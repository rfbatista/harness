package tokensavers

import (
	"bytes"
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

const gb = int64(1) << 30

type overrides struct {
	got domain.MemoryBudgetOverride
	err error
}

func (o overrides) MemoryBudgetOverride(context.Context, domain.ActivationOwner) (domain.MemoryBudgetOverride, error) {
	return o.got, o.err
}

func (o overrides) SetMemoryBudgetOverride(context.Context, domain.ActivationOwner, int64) (domain.MemoryBudgetOverride, error) {
	return o.got, o.err
}

func paint(t *testing.T, seed MemoryBudgetSeed) string {
	t.Helper()
	var b bytes.Buffer
	if err := MemoryBudget(seed).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func attr(html, name string) string {
	m := regexp.MustCompile(name + `="([^"]*)"`).FindStringSubmatch(html)
	if m == nil {
		return "<missing>"
	}
	return m[1]
}

func TestAnInheritingOwnerShowsAnEmptyFieldAndWhatItInherits(t *testing.T) {
	seed := ReadMemoryBudget(context.Background(), overrides{got: domain.MemoryBudgetOverride{Level: domain.ActivationLevelRepository, OwnerID: "r1", EffectiveBytes: 4 * gb}},
		domain.ActivationOwner{Level: domain.ActivationLevelRepository, ID: "r1"})
	html := paint(t, *seed)
	if attr(html, "data-level") != "repository" || attr(html, "data-owner-id") != "r1" || attr(html, "data-bytes") != "0" || attr(html, "data-effective") != "4294967296" {
		t.Errorf("control = %s", html)
	}
	field := regexp.MustCompile(`<input[^>]*data-memory-budget-override[^>]*>`).FindString(html)
	if field == "" || attr(field, "value") != "" {
		t.Errorf("field = %q, want an empty one", field)
	}
	if !strings.Contains(html, "Inherits 4 GB") {
		t.Errorf("the control does not say it inherits 4 GB:\n%s", html)
	}
}

func TestAnOwnerWithItsOwnOrcamentoShowsItInGB(t *testing.T) {
	seed := ReadMemoryBudget(context.Background(), overrides{got: domain.MemoryBudgetOverride{Level: domain.ActivationLevelTask, OwnerID: "t1", Bytes: gb * 5 / 2, EffectiveBytes: gb * 5 / 2}},
		domain.ActivationOwner{Level: domain.ActivationLevelTask, ID: "t1"})
	html := paint(t, *seed)
	field := regexp.MustCompile(`<input[^>]*data-memory-budget-override[^>]*>`).FindString(html)
	if attr(field, "value") != "2.5" || !strings.Contains(html, "Uses 2.5 GB") {
		t.Errorf("control = %s", html)
	}
}

func TestAnUnreadableOrcamentoLeavesTheControlOut(t *testing.T) {
	if got := ReadMemoryBudget(context.Background(), overrides{err: errors.New("down")}, domain.ActivationOwner{}); got != nil {
		t.Errorf("seed = %+v, want none", got)
	}
	if got := ReadMemoryBudget(context.Background(), nil, domain.ActivationOwner{}); got != nil {
		t.Errorf("seed without a reader = %+v, want none", got)
	}
}
