package archassistant_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"operators-mcp/internal/application/archassistant"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

func block(body string) string {
	return "Here is what I would change.\n\n```json\n" + body + "\n```"
}

const validLayer = `{"summary":"Add the domain layer.","changes":[{"target":{"kind":"layer","name":"domain"},"op":"add","value":{"name":"domain","match":{"folder":"internal/domain"},"access":{"default":"read","agents":{"software-architect":"change"}}},"reason":"internal/domain is the pure core."}]}`

const badRegexLayer = `{"summary":"Add a layer.","changes":[{"target":{"kind":"layer","name":"bad"},"op":"add","value":{"name":"bad","match":{"regex":"("},"access":{"default":"read"}},"reason":"r"}]}`

// scripted answers turn n with texts[n-1] under session s1, and records the
// inputs.
func scripted(texts ...string) func(int, archassistant.TurnCall, archassistant.TurnCallbacks) (archassistant.TurnResult, error) {
	return func(n int, _ archassistant.TurnCall, cb archassistant.TurnCallbacks) (archassistant.TurnResult, error) {
		text := texts[min(n, len(texts))-1]
		if cb.OnText != nil {
			cb.OnText(text)
		}
		return archassistant.TurnResult{SessionID: "s1", Text: text}, nil
	}
}

func result(t *testing.T, evs []ports.ArchitectureAssistantEvent) ports.ArchitectureAssistantResult {
	t.Helper()
	last := evs[len(evs)-1]
	r, ok := last.Data.(ports.ArchitectureAssistantResult)
	if last.Name != ports.ArchitectureAssistantEventResult || !ok {
		t.Fatalf("the last event is not a result: %+v", last)
	}
	return r
}

func TestAValidProposalIsReturnedWithItsBaseFilledFromTheDraft(t *testing.T) {
	e := newEnv(t)
	answer := block(`{"summary":"Set the pattern and add a layer.","changes":[` +
		`{"target":{"kind":"pattern"},"op":"change","value":"hexagonal","reason":"ports and adapters"},` +
		`{"target":{"kind":"layer","name":"domain"},"op":"add","value":{"name":"domain","match":{"folder":"internal/domain"},"access":{"default":"read"}},"reason":"pure core"}]}`)
	e.turn.script = scripted(answer)
	evs, err := e.run(context.Background(), req("set it up"))
	if err != nil {
		t.Fatal(err)
	}
	res := result(t, evs)
	p := res.Proposal
	if p == nil || !p.Valid || len(p.Errors) != 0 || p.ID == "" || p.Summary != "Set the pattern and add a layer." || len(p.Changes) != 2 {
		t.Fatalf("proposal = %+v", p)
	}
	if string(p.Changes[0].Base) != `"DRAFT-PATTERN"` {
		t.Errorf("pattern base = %s, want the draft's value", p.Changes[0].Base)
	}
	if string(p.Changes[1].Base) != "null" {
		t.Errorf("new layer base = %s, want null", p.Changes[1].Base)
	}
	if p.Changes[0].Reason != "ports and adapters" {
		t.Errorf("reason = %q", p.Changes[0].Reason)
	}
	if res.Text != answer {
		t.Errorf("the result text is not the whole answer")
	}
	if n := len(e.turn.callsMade()); n != 1 {
		t.Errorf("%d turns ran, want 1", n)
	}
}

func TestProposalIDsAreUniqueWithinTheConversation(t *testing.T) {
	e := newEnv(t)
	e.turn.script = scripted(block(validLayer))
	evs, _ := e.run(context.Background(), req("one"))
	first := result(t, evs).Proposal.ID
	r := req("two")
	r.ConversationID = conversationID(t, evs)
	evs, _ = e.run(context.Background(), r)
	if second := result(t, evs).Proposal.ID; second == first || second == "" {
		t.Errorf("ids %q and %q", first, second)
	}
}

func TestAnAnswerWithoutAProposalBlockIsPlain(t *testing.T) {
	for name, text := range map[string]string{
		"no block":            "The domain package holds the core.",
		"a block that is not": "Example:\n```json\n{\"hello\": 1}\n```",
		"broken json":         "```json\n{not json\n```",
		"no changes":          block(`{"summary":"nothing","changes":[]}`),
		"another language":    "```go\nfunc main() {}\n```",
	} {
		e := newEnv(t)
		e.turn.script = scripted(text)
		evs, err := e.run(context.Background(), req("explain"))
		if err != nil {
			t.Fatal(err)
		}
		if res := result(t, evs); res.Proposal != nil || res.Text != text {
			t.Errorf("%s: result = %+v", name, res)
		}
		if n := len(e.turn.callsMade()); n != 1 {
			t.Errorf("%s: %d turns ran, want 1", name, n)
		}
	}
}

func TestOnlyTheLastBlockCounts(t *testing.T) {
	e := newEnv(t)
	e.turn.script = scripted("First idea:\n```json\n" + badRegexLayer + "\n```\nBetter:\n" + block(validLayer))
	evs, _ := e.run(context.Background(), req("x"))
	if p := result(t, evs).Proposal; p == nil || !p.Valid || p.Changes[0].Target.Name != "domain" {
		t.Errorf("proposal = %+v", p)
	}
}

func TestAnInvalidProposalIsSentBackOnceInTheSameConversationAndTheCorrectedOneIsReturned(t *testing.T) {
	e := newEnv(t)
	e.turn.script = scripted(block(badRegexLayer), "Fixed it.\n"+block(validLayer))
	evs, err := e.run(context.Background(), req("add a layer"))
	if err != nil {
		t.Fatal(err)
	}
	calls := e.turn.callsMade()
	if len(calls) != 2 {
		t.Fatalf("%d turns ran, want 2", len(calls))
	}
	if calls[1].ResumeID != "s1" {
		t.Errorf("the retry resumed %q, want the same conversation's session", calls[1].ResumeID)
	}
	for _, want := range []string{"INVALID_REGEX", "/layers/0/match/regex"} {
		if !strings.Contains(calls[1].Input, want) {
			t.Errorf("the retry input lacks %q:\n%s", want, calls[1].Input)
		}
	}
	res := result(t, evs)
	if p := res.Proposal; p == nil || !p.Valid || len(p.Errors) != 0 || p.Changes[0].Target.Name != "domain" {
		t.Fatalf("proposal = %+v", p)
	}
	if !strings.HasPrefix(res.Text, "Fixed it.") {
		t.Errorf("text = %q, want the corrected answer", res.Text)
	}
}

func TestAProposalThatFailsTwiceIsReturnedInvalidWithItsErrorsAndNoThirdTurnRuns(t *testing.T) {
	e := newEnv(t)
	e.turn.script = scripted(block(badRegexLayer), block(badRegexLayer))
	evs, err := e.run(context.Background(), req("add a layer"))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(e.turn.callsMade()); n != 2 {
		t.Fatalf("%d turns ran, want 2", n)
	}
	p := result(t, evs).Proposal
	if p == nil || p.Valid || len(p.Changes) != 1 {
		t.Fatalf("proposal = %+v", p)
	}
	var found bool
	for _, er := range p.Errors {
		if er.Code == domain.ArchInvalidRegex && er.Path == "/layers/0/match/regex" {
			found = true
		}
	}
	if !found {
		t.Errorf("errors = %+v", p.Errors)
	}
}

func TestARetryThatAnswersWithoutAProposalLeavesTheFirstOneInvalid(t *testing.T) {
	e := newEnv(t)
	e.turn.script = scripted(block(badRegexLayer), "I could not fix it.")
	evs, _ := e.run(context.Background(), req("x"))
	if p := result(t, evs).Proposal; p == nil || p.Valid || len(p.Errors) == 0 {
		t.Errorf("proposal = %+v", p)
	}
}

func TestAChangeThatCannotBeAppliedIsAnError(t *testing.T) {
	e := newEnv(t)
	broken := block(`{"summary":"s","changes":[{"target":{"kind":"layer_access","layer":"nope","agent":"*"},"op":"change","value":"none","reason":"r"}]}`)
	e.turn.script = scripted(broken, broken)
	evs, _ := e.run(context.Background(), req("x"))
	p := result(t, evs).Proposal
	if p == nil || p.Valid || len(p.Errors) != 1 || p.Errors[0].Path != "/changes/0" || !strings.Contains(p.Errors[0].Message, "nope") {
		t.Errorf("proposal = %+v", p)
	}
}

func TestANewRuleWithoutAModeIsAWarningAndABlockingOneIsKept(t *testing.T) {
	e := newEnv(t)
	e.turn.script = scripted(block(`{"summary":"rules","changes":[` +
		`{"target":{"kind":"rule","id":"r-default"},"op":"add","value":{"id":"r-default","kind":"command","at":"before_commit","command":"make check","message":"m"},"reason":"r"},` +
		`{"target":{"kind":"rule","id":"r-block"},"op":"add","value":{"id":"r-block","kind":"command","at":"before_commit","command":"make lint","mode":"blocking","message":"m"},"reason":"r"}]}`))
	evs, _ := e.run(context.Background(), req("add rules, make lint blocking"))
	p := result(t, evs).Proposal
	if p == nil || !p.Valid {
		t.Fatalf("proposal = %+v", p)
	}
	modes := make([]string, 2)
	for i, c := range p.Changes {
		var r domain.ArchRule
		if err := json.Unmarshal(c.Value, &r); err != nil {
			t.Fatal(err)
		}
		modes[i] = r.Mode
	}
	if modes[0] != domain.ModeWarning || modes[1] != domain.ModeBlocking {
		t.Errorf("modes = %v", modes)
	}
}
