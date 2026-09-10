package orchestration

import (
	"encoding/json"
	"testing"
)

const askPayload = `{
  "questions": [
    {
      "header": "Local",
      "multiSelect": false,
      "question": "Onde devo criar o arquivo de versão?",
      "options": [
        {"label": "Raiz do repo git", "description": "É o único lugar que é de fato um projeto versionado.", "preview": "VERSION\n0.1.0"},
        {"label": "Em pessoal/", "description": "Ficará fora do git."}
      ]
    },
    {
      "header": "Extras",
      "multiSelect": true,
      "question": "O que mais devo incluir?",
      "options": [
        {"label": "CHANGELOG"},
        {"label": "Tag git"}
      ]
    }
  ]
}`

func TestParseQuestions(t *testing.T) {
	got := ParseQuestions(AskUserQuestionTool, json.RawMessage(askPayload))
	if len(got) != 2 {
		t.Fatalf("got %d questions, want 2", len(got))
	}

	first := got[0]
	if first.Header != "Local" {
		t.Errorf("header = %q, want Local", first.Header)
	}
	if first.Question != "Onde devo criar o arquivo de versão?" {
		t.Errorf("question = %q", first.Question)
	}
	if first.MultiSelect {
		t.Error("first question must be single-select")
	}
	if len(first.Options) != 2 {
		t.Fatalf("got %d options, want 2", len(first.Options))
	}
	if first.Options[0].Label != "Raiz do repo git" {
		t.Errorf("label = %q", first.Options[0].Label)
	}
	if first.Options[0].Description == "" {
		t.Error("description was dropped")
	}
	if first.Options[0].Preview != "VERSION\n0.1.0" {
		t.Errorf("preview = %q, want it preserved verbatim", first.Options[0].Preview)
	}
	if !got[1].MultiSelect {
		t.Error("second question must be multi-select")
	}
}

// A nil result is what makes the client fall back to the plain allow/deny
// approval bar, so every unusable payload must produce exactly that rather than
// an empty-but-present question list.
func TestParseQuestionsReturnsNilWhenUnusable(t *testing.T) {
	tests := []struct {
		name  string
		tool  string
		input string
	}{
		{"other tool", "Bash", `{"command":"ls"}`},
		{"tool name is case sensitive", "askuserquestion", askPayload},
		{"empty input", AskUserQuestionTool, ``},
		{"not json", AskUserQuestionTool, `not json`},
		{"not an object", AskUserQuestionTool, `["questions"]`},
		{"no questions key", AskUserQuestionTool, `{}`},
		{"empty questions", AskUserQuestionTool, `{"questions":[]}`},
		{"question with no text", AskUserQuestionTool, `{"questions":[{"options":[{"label":"a"}]}]}`},
		{"question with no options", AskUserQuestionTool, `{"questions":[{"question":"q?","options":[]}]}`},
		{"options with no labels", AskUserQuestionTool, `{"questions":[{"question":"q?","options":[{"description":"d"}]}]}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseQuestions(tc.tool, json.RawMessage(tc.input)); got != nil {
				t.Fatalf("got %+v, want nil", got)
			}
		})
	}
}

// One malformed question must not cost the user the answerable ones alongside
// it — the payload carries up to four, and dropping all of them would strand
// the session on a decision it cannot render.
func TestParseQuestionsKeepsUsableQuestions(t *testing.T) {
	input := `{"questions":[
      {"question":"broken?","options":[]},
      {"question":"fine?","options":[{"label":"yes"},{"label":"no"}]}
    ]}`

	got := ParseQuestions(AskUserQuestionTool, json.RawMessage(input))
	if len(got) != 1 {
		t.Fatalf("got %d questions, want 1", len(got))
	}
	if got[0].Question != "fine?" {
		t.Errorf("kept the wrong question: %q", got[0].Question)
	}
}

// The event JSON is the client contract; snake_case is this repo's wire style.
func TestQuestionWireFormat(t *testing.T) {
	q := Question{
		Question:    "q?",
		MultiSelect: true,
		Options:     []QuestionOption{{Label: "a"}},
	}
	b, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["multi_select"] != true {
		t.Fatalf("multi_select missing from %s", b)
	}
	if _, ok := decoded["header"]; ok {
		t.Errorf("empty header must be omitted, got %s", b)
	}
}
