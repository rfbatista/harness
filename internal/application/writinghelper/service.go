// Package writinghelper runs the writing helper's steps (ADR 0026): analyze,
// draft and titles. Each step is one stateless, tool-less claude one-shot with
// a system prompt owned by the harness. The browser holds the conversation and
// sends everything a step needs, so the service stores nothing.
package writinghelper

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"unicode/utf8"

	"operators-mcp/internal/application/agentready"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The steps and the kinds of field a step can be about.
const (
	StepAnalyze = "analyze"
	StepDraft   = "draft"
	StepTitles  = "titles"

	KindTaskTitle       = "task_title"
	KindTaskDescription = "task_description"
	KindComment         = "comment"
)

// The codes the helper's own errors carry.
const (
	CodeUnavailable = "WRITING_HELPER_UNAVAILABLE"
	CodeFailed      = "WRITING_HELPER_FAILED"
)

// Limits of a request.
const (
	maxTextLen   = 20000
	maxSiblings  = 4
	maxAnswers   = 3
	maxQuestions = 3
	maxChips     = 4
	titleCount   = 3
)

// StreamCall is one claude turn the service asks for.
type StreamCall struct {
	System, Input, Model, Attribution string
}

// Streamer runs a tool-less one-shot and gives onText each piece of the answer
// as it is written. It answers the whole text. The claudetext streaming
// adapter satisfies it, behind the readiness gate.
type Streamer interface {
	Stream(ctx context.Context, call StreamCall, onText func(string)) (string, error)
}

// ProjectReader reads the project the step is for, PROJECT_NOT_FOUND when it
// does not exist.
type ProjectReader interface {
	GetProject(ctx context.Context, projectID string) (*domain.Project, error)
}

// Field is the text the person is writing and what it is.
type Field struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// Sibling is another field of the same form.
type Sibling struct {
	Label string `json:"label"`
	Text  string `json:"text"`
}

// Answer is a question the person answered.
type Answer struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

// Request is one step, as the browser sends it.
type Request struct {
	ProjectID string    `json:"project_id"`
	Step      string    `json:"step"`
	Field     Field     `json:"field"`
	Siblings  []Sibling `json:"siblings"`
	Answers   []Answer  `json:"answers"`
	// Draft and Request make a draft step a refine.
	Draft   string   `json:"draft"`
	Request string   `json:"request"`
	Exclude []string `json:"exclude"`
}

// Question is what the analyze step asks.
type Question struct {
	ID    string   `json:"id"`
	Text  string   `json:"text"`
	Why   string   `json:"why"`
	Chips []string `json:"chips"`
}

// AnalyzeResult is the result of the analyze step.
type AnalyzeResult struct {
	AlreadyGood bool       `json:"already_good"`
	Questions   []Question `json:"questions"`
}

// DraftResult is the result of the draft step: the whole draft.
type DraftResult struct {
	Text string `json:"text"`
}

// TitlesResult is the result of the titles step.
type TitlesResult struct {
	Titles []string `json:"titles"`
}

// Delta is a piece of a draft as it is written.
type Delta struct {
	Text string `json:"text"`
}

// Event names, which the HTTP layer uses as the SSE event names.
const (
	EventDelta  = "delta"
	EventResult = "result"
)

// Event is something a step reports: a Delta (name "delta") or a result.
type Event struct {
	Name string
	Data any
}

// Availability says whether the helper can run now and, when it cannot, why
// and where to fix it.
type Availability struct {
	Available bool
	Reason    string
	FixURL    string
}

// Service runs the steps.
type Service struct {
	streamer Streamer
	settings ports.SettingsReader
	projects ProjectReader
	check    func(context.Context) error
}

// New returns the service. check is the readiness check that gates every
// agent process; nil means always ready.
func New(streamer Streamer, settings ports.SettingsReader, projects ProjectReader, check func(context.Context) error) *Service {
	return &Service{streamer: streamer, settings: settings, projects: projects, check: check}
}

// Availability runs the readiness check, without starting a step.
func (s *Service) Availability(ctx context.Context) Availability {
	if s.check == nil {
		return Availability{Available: true}
	}
	err := s.check(ctx)
	if err == nil {
		return Availability{Available: true}
	}
	u := unavailable(err)
	return Availability{Reason: u.Message, FixURL: fixURL(u)}
}

// Run runs one step and reports its events in order: for a draft, the deltas
// and then the result; otherwise the result alone. A refusal before anything
// ran, an unavailable helper or a failed step is the returned error; a
// cancelled ctx returns ctx.Err().
func (s *Service) Run(ctx context.Context, req Request, emit func(Event)) error {
	if err := validate(req); err != nil {
		return err
	}
	project, err := s.projects.GetProject(ctx, req.ProjectID)
	if err != nil {
		return err
	}
	call := StreamCall{
		System:      systemPrompt(req.Step),
		Input:       input(project.Name, req),
		Model:       domain.WritingHelperModel(s.settings.Setting(domain.SettingModelWritingHelper)),
		Attribution: "harness:writing-helper:" + req.ProjectID,
	}
	var onText func(string)
	if req.Step == StepDraft {
		onText = func(p string) { emit(Event{Name: EventDelta, Data: Delta{Text: p}}) }
	} else {
		onText = func(string) {}
	}
	text, err := s.streamer.Stream(ctx, call, onText)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if isRefusal(err) {
			return unavailable(err)
		}
		return failed("the claude turn failed: " + err.Error())
	}
	result, err := decode(req, text)
	if err != nil {
		return err
	}
	emit(Event{Name: EventResult, Data: result})
	return nil
}

func invalid(msg string) error { return &domain.StructuredError{Code: "INVALID_INPUT", Message: msg} }

func failed(msg string) error { return &domain.StructuredError{Code: CodeFailed, Message: msg} }

func validate(r Request) error {
	switch r.Step {
	case StepAnalyze, StepDraft, StepTitles:
	default:
		return invalid("step must be analyze, draft or titles")
	}
	switch r.Field.Kind {
	case KindTaskTitle, KindTaskDescription, KindComment:
	default:
		return invalid("field.kind must be task_title, task_description or comment")
	}
	if strings.TrimSpace(r.ProjectID) == "" {
		return invalid("project_id is required")
	}
	if len(r.Siblings) > maxSiblings {
		return invalid("at most 4 siblings")
	}
	if len(r.Answers) > maxAnswers {
		return invalid("at most 3 answers")
	}
	texts := []string{r.Field.Text, r.Draft, r.Request}
	for _, sb := range r.Siblings {
		texts = append(texts, sb.Text)
	}
	for _, t := range texts {
		if utf8.RuneCountInString(t) > maxTextLen {
			return invalid("a text is longer than 20000 characters")
		}
	}
	return nil
}

// isRefusal says whether err is the readiness gate refusing, or the CLI
// missing: the helper cannot run, as opposed to a step that failed.
func isRefusal(err error) bool {
	if errors.Is(err, exec.ErrNotFound) {
		return true
	}
	var se *domain.StructuredError
	if !errors.As(err, &se) {
		return false
	}
	switch se.Code {
	case agentready.CodeCredentialMissing, agentready.CodeClaudeMissing, agentready.CodeClaudeInstalling,
		agentready.CodeClaudeFailed, agentready.CodeSandboxMissing, "CLAUDE_CLI_NOT_FOUND":
		return true
	}
	return false
}

// unavailable is the WRITING_HELPER_UNAVAILABLE error for a refusal, with the
// gate's message and the anchor of the page section that fixes it.
func unavailable(err error) *domain.DetailedError {
	msg, fix := err.Error(), agentready.AnchorClaude
	var se *domain.StructuredError
	if errors.As(err, &se) {
		msg = se.Message
	}
	var de *domain.DetailedError
	if errors.As(err, &de) {
		if d, ok := de.Details.(map[string]any); ok {
			if missing, ok := d["missing"].([]map[string]string); ok && len(missing) > 0 {
				fix = missing[0]["anchor"]
			}
		}
	}
	if errors.Is(err, exec.ErrNotFound) {
		msg = "the managed claude is not installed. Install it in Agent isolation settings"
	}
	return &domain.DetailedError{
		StructuredError: &domain.StructuredError{Code: CodeUnavailable, Message: msg},
		Details:         map[string]string{"fix_url": fix},
	}
}

// fixURL reads the fix URL of an unavailable error.
func fixURL(e *domain.DetailedError) string {
	d, _ := e.Details.(map[string]string)
	return d["fix_url"]
}

// FixURL is the page anchor that fixes err when it is the helper's
// unavailable error, "" otherwise.
func FixURL(err error) string {
	var de *domain.DetailedError
	if errors.As(err, &de) {
		return fixURL(de)
	}
	return ""
}

// decode reads a step's answer into its result.
func decode(req Request, text string) (any, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, failed("the helper answered with nothing")
	}
	switch req.Step {
	case StepDraft:
		return DraftResult{Text: text}, nil
	case StepAnalyze:
		var r AnalyzeResult
		if err := json.Unmarshal([]byte(stripFence(text)), &r); err != nil {
			return nil, failed("the helper's answer could not be read")
		}
		if r.AlreadyGood || len(r.Questions) == 0 {
			r.Questions = []Question{}
		}
		if len(r.Questions) > maxQuestions {
			r.Questions = r.Questions[:maxQuestions]
		}
		for i := range r.Questions {
			q := &r.Questions[i]
			if q.ID == "" {
				q.ID = "q" + string(rune('1'+i))
			}
			if q.Chips == nil {
				q.Chips = []string{}
			}
			if len(q.Chips) > maxChips {
				q.Chips = q.Chips[:maxChips]
			}
		}
		return r, nil
	default:
		var r TitlesResult
		if err := json.Unmarshal([]byte(stripFence(text)), &r); err != nil {
			return nil, failed("the helper's answer could not be read")
		}
		if len(r.Titles) != titleCount {
			return nil, failed("the helper did not give exactly 3 titles")
		}
		seen := map[string]bool{}
		for _, e := range req.Exclude {
			seen[norm(e)] = true
		}
		for i, t := range r.Titles {
			r.Titles[i] = strings.TrimSpace(t)
			if r.Titles[i] == "" || seen[norm(t)] {
				return nil, failed("the helper gave an empty or repeated title")
			}
		}
		return r, nil
	}
}

func norm(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// stripFence removes a markdown code fence around a JSON answer.
func stripFence(s string) string {
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimPrefix(s, "json")
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "```"))
}
