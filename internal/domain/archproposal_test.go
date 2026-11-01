package domain

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func proposalRecord() ArchitectureRecord {
	r := ArchitectureRecord{
		Repository: "harness",
		Pattern:    "hexagonal",
		Layers: []ArchLayer{
			{Name: "domain", Match: ArchLayerMatch{Folder: "internal/domain"}, Purpose: "core",
				Access: LayerAccess{Default: AccessRead, Agents: map[string]AccessLevel{"software-architect": AccessChange}}},
			{Name: "app", Match: ArchLayerMatch{Folder: "internal/app"}, Access: LayerAccess{Default: AccessRead}},
		},
		Unlayered:    LayerAccess{Default: AccessRead, Agents: map[string]AccessLevel{"devops": AccessChange}},
		Rules:        []ArchRule{{ID: "r1", Kind: RuleEditOutsideLayer, Mode: ModeWarning, Message: "m"}},
		Tools:        []ArchTool{{Name: "github", Kind: "mcp", UsedBy: []string{"all"}}},
		Sandboxes:    []ArchSandbox{{Name: "seatbelt"}},
		Instructions: []ArchInstruction{{Path: "CLAUDE.md", Kind: "rules", Loaded: "always"}},
		Orchestration: ArchOrchestration{
			Routing:  []ArchRoute{{Agent: "go-developer", Model: "sonnet"}},
			Handoffs: []ArchHandoff{{From: "a", To: "b", When: "done"}},
		},
	}
	r.Normalize()
	return r
}

func change(target, op, value string) ProposalChange {
	c := ProposalChange{Op: op, Reason: "because"}
	if err := json.Unmarshal([]byte(target), &c.Target); err != nil {
		panic(err)
	}
	if value != "" {
		c.Value = json.RawMessage(value)
	}
	return c
}

func applied(t *testing.T, changes ...ProposalChange) ArchitectureRecord {
	t.Helper()
	got, err := ApplyProposal(proposalRecord(), changes)
	if err != nil {
		t.Fatalf("ApplyProposal: %v", err)
	}
	return got
}

func TestAProposalAddsAndRemovesThePattern(t *testing.T) {
	if got := applied(t, change(`{"kind":"pattern"}`, "change", `"clean"`)); got.Pattern != "clean" {
		t.Errorf("pattern = %q", got.Pattern)
	}
	if got := applied(t, change(`{"kind":"pattern"}`, "remove", "")); got.Pattern != "" {
		t.Errorf("pattern = %q after remove", got.Pattern)
	}
}

func TestANewLayerIsAppendedWithItsAccess(t *testing.T) {
	got := applied(t, change(`{"kind":"layer","name":"adapters"}`, "add",
		`{"name":"adapters","match":{"folder":"internal/adapter"},"access":{"default":"none","agents":{"go-developer":"change"}}}`))
	if len(got.Layers) != 3 || got.Layers[2].Name != "adapters" {
		t.Fatalf("layers = %+v", got.Layers)
	}
	if a := got.Layers[2].Access; a.Default != AccessNone || a.Agents["go-developer"] != AccessChange {
		t.Errorf("access = %+v", a)
	}
}

func TestALayerChangeReplacesEverythingButAccess(t *testing.T) {
	got := applied(t, change(`{"kind":"layer","name":"domain"}`, "change",
		`{"name":"domain","match":{"folder":"internal/core"},"purpose":"pure core","access":{"default":"none"}}`))
	l := got.Layers[0]
	if l.Match.Folder != "internal/core" || l.Purpose != "pure core" {
		t.Errorf("layer = %+v", l)
	}
	if l.Access.Default != AccessRead || l.Access.Agents["software-architect"] != AccessChange {
		t.Errorf("access was changed: %+v", l.Access)
	}
}

func TestARenameIsARemoveAndAnAdd(t *testing.T) {
	got := applied(t,
		change(`{"kind":"layer","name":"app"}`, "remove", ""),
		change(`{"kind":"layer","name":"application"}`, "add", `{"name":"application","match":{"folder":"internal/app"},"access":{"default":"read"}}`))
	if len(got.Layers) != 2 || got.Layers[0].Name != "domain" || got.Layers[1].Name != "application" {
		t.Errorf("layers = %+v", got.Layers)
	}
}

func TestLayerAccessSetsAnAgentTheDefaultAndRemovesAnEntry(t *testing.T) {
	got := applied(t,
		change(`{"kind":"layer_access","layer":"domain","agent":"go-developer"}`, "add", `"change"`),
		change(`{"kind":"layer_access","layer":"domain","agent":"*"}`, "change", `"none"`),
		change(`{"kind":"layer_access","layer":"domain","agent":"software-architect"}`, "remove", ""))
	a := got.Layers[0].Access
	if a.Default != AccessNone || a.Agents["go-developer"] != AccessChange {
		t.Errorf("access = %+v", a)
	}
	if _, ok := a.Agents["software-architect"]; ok {
		t.Errorf("the removed entry is still there: %+v", a)
	}
	if a.For("software-architect") != AccessNone {
		t.Errorf("a removed entry should fall back to the default")
	}
}

func TestRemovingTheDefaultCellDoesNothing(t *testing.T) {
	got := applied(t,
		change(`{"kind":"layer_access","layer":"domain","agent":"*"}`, "remove", ""),
		change(`{"kind":"unlayered_access","agent":"*"}`, "remove", ""))
	if !reflect.DeepEqual(got, proposalRecord()) {
		t.Errorf("record changed: %+v", got)
	}
}

func TestUnlayeredAccessFollowsTheSameRules(t *testing.T) {
	got := applied(t,
		change(`{"kind":"unlayered_access","agent":"*"}`, "change", `"none"`),
		change(`{"kind":"unlayered_access","agent":"go-developer"}`, "add", `"read"`),
		change(`{"kind":"unlayered_access","agent":"devops"}`, "remove", ""))
	a := got.Unlayered
	if a.Default != AccessNone || a.Agents["go-developer"] != AccessRead || len(a.Agents) != 1 {
		t.Errorf("unlayered = %+v", a)
	}
}

func TestAccessOfAnUnknownLayerIsRefused(t *testing.T) {
	_, err := ApplyProposal(proposalRecord(), []ProposalChange{change(`{"kind":"layer_access","layer":"nope","agent":"*"}`, "change", `"none"`)})
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Errorf("err = %v", err)
	}
}

func TestEveryOtherTargetKindAddsChangesAndRemoves(t *testing.T) {
	got := applied(t,
		change(`{"kind":"rule","id":"r2"}`, "add", `{"id":"r2","kind":"command","at":"before_commit","command":"make check","mode":"warning","message":"m"}`),
		change(`{"kind":"rule","id":"r1"}`, "change", `{"id":"r1","kind":"edit_outside_layer","mode":"blocking","message":"new"}`),
		change(`{"kind":"tool","name":"postgres"}`, "add", `{"name":"postgres","kind":"mcp","used_by":["all"],"writes":false}`),
		change(`{"kind":"tool","name":"github"}`, "remove", ""),
		change(`{"kind":"sandbox","name":"bwrap"}`, "add", `{"name":"bwrap","can_reach":[],"cannot_reach":[],"used_by":["all"]}`),
		change(`{"kind":"instruction","path":"AGENTS.md"}`, "add", `{"path":"AGENTS.md","kind":"rules","applies_to":["all"],"loaded":"always"}`),
		change(`{"kind":"routing","agent":"go-developer"}`, "change", `{"agent":"go-developer","model":"opus"}`),
		change(`{"kind":"handoff","from":"a","to":"c"}`, "add", `{"from":"a","to":"c","when":"later"}`),
		change(`{"kind":"handoff","from":"a","to":"b"}`, "remove", ""))
	if len(got.Rules) != 2 || got.Rules[0].Mode != ModeBlocking || got.Rules[0].Message != "new" || got.Rules[1].ID != "r2" {
		t.Errorf("rules = %+v", got.Rules)
	}
	if len(got.Tools) != 1 || got.Tools[0].Name != "postgres" {
		t.Errorf("tools = %+v", got.Tools)
	}
	if len(got.Sandboxes) != 2 || got.Sandboxes[1].Name != "bwrap" {
		t.Errorf("sandboxes = %+v", got.Sandboxes)
	}
	if len(got.Instructions) != 2 || got.Instructions[1].Path != "AGENTS.md" {
		t.Errorf("instructions = %+v", got.Instructions)
	}
	if got.Orchestration.Routing[0].Model != "opus" {
		t.Errorf("routing = %+v", got.Orchestration.Routing)
	}
	if hs := got.Orchestration.Handoffs; len(hs) != 1 || hs[0].To != "c" {
		t.Errorf("handoffs = %+v", hs)
	}
}

func TestApplyingAProposalNeverChangesItsInputAndIsRepeatable(t *testing.T) {
	in := proposalRecord()
	changes := []ProposalChange{
		change(`{"kind":"layer_access","layer":"domain","agent":"go-developer"}`, "add", `"change"`),
		change(`{"kind":"layer","name":"app"}`, "remove", ""),
	}
	first, err := ApplyProposal(in, changes)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, proposalRecord()) {
		t.Fatalf("the input was mutated: %+v", in)
	}
	second, _ := ApplyProposal(in, changes)
	if !reflect.DeepEqual(first, second) {
		t.Errorf("not repeatable: %+v vs %+v", first, second)
	}
}

func TestAProposalIsValidatedWithTheRecordsOwnValidation(t *testing.T) {
	got := applied(t, change(`{"kind":"layer","name":"bad"}`, "add", `{"name":"bad","match":{"regex":"("},"access":{"default":"read"}}`))
	errs := got.Validate(ArchitectureKnown{Agents: map[string]bool{"software-architect": true, "devops": true}})
	var found bool
	for _, e := range errs {
		if e.Code == ArchInvalidRegex && e.Path == "/layers/2/match/regex" {
			found = true
		}
	}
	if !found {
		t.Errorf("errors = %+v, want INVALID_REGEX at /layers/2/match/regex", errs)
	}
}

func TestABadTargetOrValueIsRefused(t *testing.T) {
	for name, c := range map[string]ProposalChange{
		"unknown kind":        change(`{"kind":"nope"}`, "add", `{}`),
		"unknown op":          change(`{"kind":"pattern"}`, "frob", `"x"`),
		"missing name":        change(`{"kind":"layer"}`, "add", `{"name":"x"}`),
		"value not an object": change(`{"kind":"layer","name":"x"}`, "add", `"text"`),
		"missing value":       change(`{"kind":"tool","name":"x"}`, "add", ``),
		"bad level":           change(`{"kind":"unlayered_access","agent":"x"}`, "add", `"admin"`),
	} {
		if _, err := ApplyProposal(proposalRecord(), []ProposalChange{c}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestABaseIsTheTargetsValueInARecordOrNull(t *testing.T) {
	r := proposalRecord()
	base := func(target, op string) string {
		t.Helper()
		b, err := ProposalBase(r, change(target, op, "").Target, op)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if got := base(`{"kind":"layer","name":"nope"}`, "add"); got != "null" {
		t.Errorf("missing layer base = %s", got)
	}
	if got := base(`{"kind":"pattern"}`, "change"); got != `"hexagonal"` {
		t.Errorf("pattern base = %s", got)
	}
	if got := base(`{"kind":"layer_access","layer":"domain","agent":"software-architect"}`, "change"); got != `"change"` {
		t.Errorf("access base = %s", got)
	}
	if got := base(`{"kind":"layer_access","layer":"domain","agent":"*"}`, "change"); got != `"read"` {
		t.Errorf("default base = %s", got)
	}
	if got := base(`{"kind":"layer_access","layer":"domain","agent":"nobody"}`, "add"); got != "null" {
		t.Errorf("missing cell base = %s", got)
	}
	if got := base(`{"kind":"rule","id":"r1"}`, "change"); !strings.Contains(got, `"id":"r1"`) {
		t.Errorf("rule base = %s", got)
	}
	// A layer change leaves access alone, so its base has none.
	got := base(`{"kind":"layer","name":"domain"}`, "change")
	if strings.Contains(got, `"access"`) || !strings.Contains(got, `"purpose":"core"`) {
		t.Errorf("layer change base = %s", got)
	}
	// A layer remove takes the whole layer with it.
	if got := base(`{"kind":"layer","name":"domain"}`, "remove"); !strings.Contains(got, `"access"`) {
		t.Errorf("layer remove base = %s", got)
	}
}
