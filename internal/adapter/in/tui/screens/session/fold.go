package session

// fold.go is the pure part of the screen: events in, timeline out. It has no
// UI state and mirrors the Flutter SessionTimelineBuilder so both front-ends
// read a session the same way.

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"time"

	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/domain"
)

// ItemKind is what a timeline row represents.
type ItemKind int

const (
	ItemStatus ItemKind = iota // lifecycle divider
	ItemUser
	ItemAssistant
	ItemTool
	ItemApproval
	ItemResolved
	ItemAutoRun
	ItemError
	ItemEnded
)

// Item is one renderable timeline entry.
type Item struct {
	Kind ItemKind
	Seq  int64
	At   time.Time
	Text string

	Status domain.SessionStatus // ItemStatus, ItemEnded, ItemError (when terminal)

	// ItemTool and ItemApproval
	ToolName string
	Summary  string
	Input    json.RawMessage
	Result   string

	// ItemApproval
	ReqID     string
	Questions []orchestration.Question

	// ItemResolved
	Expired bool
	// ItemAutoRun
	Enabled bool
}

// IsQuestion reports whether an approval wants answers rather than a permission.
func (it Item) IsQuestion() bool { return len(it.Questions) > 0 }

// Usage is the running total folded out of usage events.
type Usage struct {
	CostUSD      float64
	InputTokens  int
	OutputTokens int
}

// Timeline is the folded view of a session's durable events.
type Timeline struct {
	Items   []Item
	Pending []Item // approvals still awaiting a decision, oldest first
	Status  domain.SessionStatus
	AutoRun bool
	Usage   Usage
}

// isPending reports whether an approval request still awaits a decision.
func (tl Timeline) isPending(reqID string) bool {
	for _, p := range tl.Pending {
		if p.ReqID == reqID {
			return true
		}
	}
	return false
}

// isTerminal mirrors the Flutter SessionTerminal rule: done always ends the
// session; an error only when it carries the failed status.
func isTerminal(ev orchestration.SessionEvent) bool {
	switch ev.Type {
	case "done":
		return true
	case "error":
		return ev.Status == domain.SessionFailed
	}
	return false
}

// Fold turns events into a timeline. Events are sorted by seq; deltas and
// unknown types are ignored.
func Fold(events []orchestration.SessionEvent) Timeline {
	ordered := append([]orchestration.SessionEvent(nil), events...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Seq < ordered[j].Seq })

	var tl Timeline
	var unresolved []int // indexes into tl.Items of tool calls awaiting a result
	pending := map[string]int{}
	var pendingOrder []string
	var lastStatus domain.SessionStatus
	var lastAssistant string

	retire := func(reqID string) {
		if reqID == "" {
			if len(pendingOrder) > 0 {
				reqID = pendingOrder[0]
			} else {
				return
			}
		}
		if _, ok := pending[reqID]; !ok {
			return
		}
		delete(pending, reqID)
		for i, id := range pendingOrder {
			if id == reqID {
				pendingOrder = append(pendingOrder[:i], pendingOrder[i+1:]...)
				break
			}
		}
	}
	clearPending := func() {
		pending = map[string]int{}
		pendingOrder = nil
	}

	for _, ev := range ordered {
		terminal := ev.Type == "done" || ev.Type == "error"
		if ev.Status != "" && ev.Status != lastStatus {
			if !terminal {
				tl.Items = append(tl.Items, Item{Kind: ItemStatus, Seq: ev.Seq, At: ev.At, Status: ev.Status})
			}
			lastStatus = ev.Status
		}
		switch ev.Type {
		case "user_message":
			if text := strings.TrimSpace(ev.Text); text != "" {
				tl.Items = append(tl.Items, Item{Kind: ItemUser, Seq: ev.Seq, At: ev.At, Text: text})
			}
		case "output":
			if text := strings.TrimSpace(ev.Text); text != "" {
				tl.Items = append(tl.Items, Item{Kind: ItemAssistant, Seq: ev.Seq, At: ev.At, Text: text})
				lastAssistant = text
			}
		case "tool_use":
			name := ev.ToolName
			if name == "" {
				name = "tool"
			}
			tl.Items = append(tl.Items, Item{Kind: ItemTool, Seq: ev.Seq, At: ev.At, ToolName: name, Summary: InputSummary(name, ev.ToolInput), Input: ev.ToolInput})
			unresolved = append(unresolved, len(tl.Items)-1)
		case "tool_result":
			result := decodeToolResult(ev.Text)
			if len(unresolved) > 0 {
				tl.Items[unresolved[0]].Result = result
				unresolved = unresolved[1:]
			} else {
				tl.Items = append(tl.Items, Item{Kind: ItemTool, Seq: ev.Seq, At: ev.At, ToolName: "result", Result: result})
			}
		case "approval_needed":
			if ev.Approval == nil {
				continue
			}
			a := ev.Approval
			it := Item{Kind: ItemApproval, Seq: ev.Seq, At: ev.At, ReqID: a.ReqID, ToolName: a.ToolName, Summary: InputSummary(a.ToolName, a.Input), Input: a.Input, Questions: a.Questions}
			tl.Items = append(tl.Items, it)
			pending[a.ReqID] = len(tl.Items) - 1
			pendingOrder = append(pendingOrder, a.ReqID)
		case "approval_resolved", "approval_expired":
			reqID := ""
			if ev.Approval != nil {
				reqID = ev.Approval.ReqID
			}
			retire(reqID)
			tl.Items = append(tl.Items, Item{Kind: ItemResolved, Seq: ev.Seq, At: ev.At, Text: ev.Text, ReqID: reqID, Expired: ev.Type == "approval_expired"})
		case "auto_run":
			if ev.AutoRun != nil {
				tl.AutoRun = *ev.AutoRun
				tl.Items = append(tl.Items, Item{Kind: ItemAutoRun, Seq: ev.Seq, At: ev.At, Enabled: *ev.AutoRun})
			}
		case "usage":
			tl.Usage.CostUSD += ev.CostUSD
			if ev.Usage != nil {
				tl.Usage.InputTokens += ev.Usage.InputTokens
				tl.Usage.OutputTokens += ev.Usage.OutputTokens
			}
			if text := strings.TrimSpace(ev.Text); text != "" && text != lastAssistant {
				tl.Items = append(tl.Items, Item{Kind: ItemAssistant, Seq: ev.Seq, At: ev.At, Text: text})
				lastAssistant = text
			}
		case "done":
			st := ev.Status
			if st == "" {
				st = domain.SessionDone
			}
			tl.Items = append(tl.Items, Item{Kind: ItemEnded, Seq: ev.Seq, At: ev.At, Status: st})
			if isTerminal(ev) {
				clearPending()
			}
		case "error":
			tl.Items = append(tl.Items, Item{Kind: ItemError, Seq: ev.Seq, At: ev.At, Text: ev.Text, Status: ev.Status})
			if isTerminal(ev) {
				clearPending()
			}
		}
	}
	tl.Status = lastStatus
	for _, id := range pendingOrder {
		tl.Pending = append(tl.Pending, tl.Items[pending[id]])
	}
	return tl
}

// decodeToolResult unwraps a JSON string literal; anything else is kept raw.
func decodeToolResult(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return raw
	}
	var s string
	if err := json.Unmarshal([]byte(trimmed), &s); err == nil {
		return s
	}
	return raw
}

var whitespace = regexp.MustCompile(`\s+`)

const summaryMax = 120

// InputSummary is the one-line feed summary of a tool input: the tool's
// primary argument when known, else the first string value.
func InputSummary(toolName string, input json.RawMessage) string {
	if len(input) == 0 {
		return ""
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(input, &obj); err != nil || len(obj) == 0 {
		return ""
	}
	if key := summaryKey(toolName); key != "" {
		if s := cleanString(obj[key]); s != "" {
			return s
		}
	}
	// Map iteration is random; pick the first string value by key order so
	// the summary is stable across renders.
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if s := cleanString(obj[k]); s != "" {
			return s
		}
	}
	return ""
}

func summaryKey(tool string) string {
	switch tool {
	case "Bash":
		return "command"
	case "Read", "Write", "Edit", "NotebookEdit":
		return "file_path"
	case "Glob", "Grep":
		return "pattern"
	case "WebFetch":
		return "url"
	case "Task", "Agent":
		return "description"
	}
	return ""
}

func cleanString(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	flat := strings.TrimSpace(whitespace.ReplaceAllString(s, " "))
	if len(flat) > summaryMax {
		return flat[:summaryMax-1] + "…"
	}
	return flat
}

// Deltas is the live streaming tail: text keyed by content-block index and
// kind, cleared when the durable output lands.
type Deltas struct {
	blocks map[deltaKey]*strings.Builder
	order  []deltaKey
}

type deltaKey struct {
	index int
	kind  string
}

// Apply folds an event into the tail: deltas accumulate, an output clears.
func (d *Deltas) Apply(ev orchestration.SessionEvent) {
	switch ev.Type {
	case "output_delta":
		if d.blocks == nil {
			d.blocks = map[deltaKey]*strings.Builder{}
		}
		k := deltaKey{ev.Index, ev.DeltaKind}
		b := d.blocks[k]
		if b == nil {
			b = &strings.Builder{}
			d.blocks[k] = b
			d.order = append(d.order, k)
		}
		b.WriteString(ev.Text)
	case "output", "done", "error", "tool_use":
		d.blocks, d.order = nil, nil
	}
}

// Empty reports whether nothing is streaming.
func (d *Deltas) Empty() bool { return len(d.order) == 0 }

func (d *Deltas) join(kind string) string {
	var parts []string
	for _, k := range d.order {
		if k.kind == kind {
			parts = append(parts, d.blocks[k].String())
		}
	}
	return strings.Join(parts, "")
}

// Text is the streaming assistant text.
func (d *Deltas) Text() string { return d.join("text") }

// Thinking is the streaming reasoning text.
func (d *Deltas) Thinking() string { return d.join("thinking") }

// ToolInput is the streaming tool input JSON.
func (d *Deltas) ToolInput() string { return d.join("tool_input") }
