package domain

import (
	"errors"
	"strings"
	"testing"
)

// One row per mode, in the contract's order: blocked while the key is unset
// or `on`, not blocked only when it is `off`.
func TestBranchBlocks(t *testing.T) {
	stored := map[string]string{"branches.block.mode.design": "off", "branches.block.mode.product": "on"}
	got := BranchBlocks(func(k string) string { return stored[k] })
	want := []BranchBlock{
		{SessionModeDefault, "branches.block.mode.default", "Default", true},
		{SessionModeArchitect, "branches.block.mode.architect", "Architect", true},
		{SessionModeImplement, "branches.block.mode.implement", "Implement", true},
		{SessionModeDesign, "branches.block.mode.design", "Design", false},
		{SessionModeProduct, "branches.block.mode.product", "Product", true},
		{SessionModeDesignSystem, "branches.block.mode.designsystem", "Design system", true},
		{SessionModeReview, "branches.block.mode.review", "Reviewer", true},
		{SessionModeTest, "branches.block.mode.test", "Tester", true},
		{SessionModeQA, "branches.block.mode.qa", "QA", true},
		{SessionModeOperate, "branches.block.mode.operate", "Operator", true},
		{SessionModeProductOwner, "branches.block.mode.productowner", "Product owner", true},
	}
	if len(got) != len(want) {
		t.Fatalf("BranchBlocks = %+v; want %d rows", got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %+v; want %+v", i, got[i], want[i])
		}
	}
}

// A mode that is not one of the six follows the Default key.
func TestBranchBlockedForAnUnknownModeFollowsDefault(t *testing.T) {
	read := func(k string) string {
		if k == "branches.block.mode.default" {
			return "off"
		}
		return ""
	}
	if blocked, name := BranchBlockedFor(SessionMode("mystery"), read); blocked || name != "Default" {
		t.Errorf("BranchBlockedFor(mystery) = %v, %q; want false, Default", blocked, name)
	}
	if blocked, name := BranchBlockedFor(SessionModeArchitect, read); !blocked || name != "Architect" {
		t.Errorf("BranchBlockedFor(architect) = %v, %q; want true, Architect", blocked, name)
	}
}

func TestValidateBranchBlockSetting(t *testing.T) {
	for _, mode := range []string{"default", "architect", "implement", "design", "product", "designsystem", "review", "test", "qa", "operate"} {
		key := "branches.block.mode." + mode
		for _, ok := range []string{"on", "off", ""} {
			if err := ValidateSetting(key, ok); err != nil {
				t.Errorf("%s=%q: %v", key, ok, err)
			}
		}
		err := ValidateSetting(key, "maybe")
		var se *StructuredError
		if !errors.As(err, &se) || se.Code != "INVALID_INPUT" || !strings.Contains(se.Message, key) || !strings.Contains(se.Message, "on") || !strings.Contains(se.Message, "off") {
			t.Errorf("%s=maybe: %v; want INVALID_INPUT naming the key and on/off", key, err)
		}
	}
}

// The message is the contract's, word for word.
func TestBranchBlockedMessage(t *testing.T) {
	got := BranchBlockedMessage("Design system", "agent/x")
	want := "BRANCH_ACTION_BLOCKED: creating, switching and deleting branches is blocked for Design system sessions. Stay on your session branch agent/x. Pushing still goes through a branch request."
	if got != want {
		t.Errorf("message = %q; want %q", got, want)
	}
}
