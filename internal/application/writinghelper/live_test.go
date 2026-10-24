//go:build live

// Live check of the writing helper against the real Claude. It runs by hand:
//
//	go test -tags live -run TestLive -v -timeout 20m ./internal/application/writinghelper
//
// It uses the claude on PATH (or $CLAUDE_BIN) and whatever login it has, calls
// the real API, and costs a few cents. It runs every sample of
// testdata/samples.json through analyze, draft and titles and asserts what
// the contract promises: no invented facts, the person's language, at most 3
// questions with at most 4 chips, and exactly 3 titles that "Three more" does
// not repeat.
package writinghelper_test

import (
	"context"
	_ "embed"
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/rfbatista/llmkit"
	"github.com/rfbatista/llmkit/claude"

	"operators-mcp/internal/application/writinghelper"
	"operators-mcp/internal/domain"
)

//go:embed testdata/samples.json
var samplesJSON []byte

type sample struct {
	Name      string                  `json:"name"`
	Language  string                  `json:"language"`
	Field     writinghelper.Field     `json:"field"`
	Siblings  []writinghelper.Sibling `json:"siblings"`
	Forbidden []string                `json:"forbidden"`
}

type liveStreamer struct{ m *claude.Manager }

func (s liveStreamer) Stream(ctx context.Context, c writinghelper.StreamCall, onText func(string)) (string, error) {
	res, err := s.m.OneShotStream(ctx, claude.OneShotRequest{System: c.System, Input: c.Input, Model: c.Model}, onText)
	return res.Text, err
}

type liveSettings struct{}

func (liveSettings) Setting(string) string { return os.Getenv("WRITING_HELPER_MODEL") }

type liveProjects struct{}

func (liveProjects) GetProject(context.Context, string) (*domain.Project, error) {
	return &domain.Project{ID: "live", Name: "Orion"}, nil
}

var (
	ptWords = regexp.MustCompile(`(?i)\b(o|a|os|as|de|do|da|que|não|nao|para|com|uma|um|é|em|no|na|por|se)\b`)
	enWords = regexp.MustCompile(`(?i)\b(the|and|to|is|for|with|of|that|in|on|it|are|this|be)\b`)
	numbers = regexp.MustCompile(`\d+`)
	listNum = regexp.MustCompile(`(?m)^\s*\d+[.)]\s`)
)

func languageOf(s string) string {
	if len(ptWords.FindAllString(s, -1)) > len(enWords.FindAllString(s, -1)) {
		return "pt"
	}
	return "en"
}

func TestLive(t *testing.T) {
	var samples []sample
	if err := json.Unmarshal(samplesJSON, &samples); err != nil {
		t.Fatal(err)
	}
	bin := os.Getenv("CLAUDE_BIN")
	if bin == "" {
		bin = "claude"
	}
	svc := writinghelper.New(liveStreamer{claude.New(llmkit.Options{Bin: bin})}, liveSettings{}, liveProjects{}, nil)

	for _, s := range samples {
		t.Run(s.Name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
			defer cancel()
			req := writinghelper.Request{ProjectID: "live", Field: s.Field, Siblings: s.Siblings}
			input := s.Field.Text + " " + joinSiblings(s.Siblings)
			if s.Field.Kind == "task_title" {
				liveTitles(ctx, t, svc, req, s)
				return
			}
			liveAnalyze(ctx, t, svc, req, s)
			liveDraft(ctx, t, svc, req, s, input)
		})
	}
}

func run(ctx context.Context, t *testing.T, svc *writinghelper.Service, req writinghelper.Request) (any, string) {
	t.Helper()
	var result any
	var deltas strings.Builder
	err := svc.Run(ctx, req, func(e writinghelper.Event) {
		if d, ok := e.Data.(writinghelper.Delta); ok {
			deltas.WriteString(d.Text)
		} else {
			result = e.Data
		}
	})
	if err != nil {
		t.Fatalf("%s: %v", req.Step, err)
	}
	return result, deltas.String()
}

func liveAnalyze(ctx context.Context, t *testing.T, svc *writinghelper.Service, req writinghelper.Request, s sample) {
	req.Step = writinghelper.StepAnalyze
	res, _ := run(ctx, t, svc, req)
	a := res.(writinghelper.AnalyzeResult)
	if len(a.Questions) > 3 {
		t.Errorf("%d questions, want at most 3", len(a.Questions))
	}
	var all []string
	for _, q := range a.Questions {
		if len(q.Chips) > 4 {
			t.Errorf("question %q has %d chips", q.Text, len(q.Chips))
		}
		all = append(all, q.Text, q.Why)
	}
	if len(all) > 0 && languageOf(strings.Join(all, " ")) != s.Language {
		t.Errorf("questions are not in %s: %v", s.Language, all)
	}
	t.Logf("already_good=%v questions=%d", a.AlreadyGood, len(a.Questions))
}

func liveDraft(ctx context.Context, t *testing.T, svc *writinghelper.Service, req writinghelper.Request, s sample, input string) {
	req.Step = writinghelper.StepDraft
	res, deltas := run(ctx, t, svc, req)
	draft := res.(writinghelper.DraftResult).Text
	if deltas != draft && strings.TrimSpace(deltas) != draft {
		t.Errorf("the deltas do not add up to the draft:\n%q\n%q", deltas, draft)
	}
	t.Logf("draft:\n%s", draft)
	// Facts the person did not give must be open placeholders: take them out
	// before looking for invented ones.
	body := regexp.MustCompile(`(?s)\[open:.*?\]`).ReplaceAllString(draft, "")
	lower, in := strings.ToLower(body), strings.ToLower(input)
	for _, word := range s.Forbidden {
		if strings.Contains(lower, word) && !strings.Contains(in, word) {
			t.Errorf("the draft invents %q", word)
		}
	}
	for _, n := range numbers.FindAllString(listNum.ReplaceAllString(body, ""), -1) {
		if !strings.Contains(in, n) {
			t.Errorf("the draft has the number %s, which is not in the input", n)
		}
	}
	if languageOf(body) != s.Language {
		t.Errorf("the draft is not in %s", s.Language)
	}
}

func liveTitles(ctx context.Context, t *testing.T, svc *writinghelper.Service, req writinghelper.Request, s sample) {
	req.Step = writinghelper.StepTitles
	res, _ := run(ctx, t, svc, req)
	first := res.(writinghelper.TitlesResult).Titles
	req.Exclude = first
	res, _ = run(ctx, t, svc, req)
	more := res.(writinghelper.TitlesResult).Titles
	if len(first) != 3 || len(more) != 3 {
		t.Fatalf("titles = %v then %v, want 3 each", first, more)
	}
	if languageOf(strings.Join(append(first, more...), " ")) != s.Language {
		t.Errorf("titles are not in %s: %v %v", s.Language, first, more)
	}
	t.Logf("titles: %v / %v", first, more)
}

func joinSiblings(ss []writinghelper.Sibling) string {
	var parts []string
	for _, s := range ss {
		parts = append(parts, s.Text)
	}
	return strings.Join(parts, " ")
}
