package claudehome

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"operators-mcp/internal/ports"
)

// The mapping from the CLI's JSONL records to transcript entries. The record
// format is the CLI's own and changes between versions, so everything here
// is defensive: an unknown record, part or shape yields nothing rather than
// an error. The plan "Harness server — agent session history" has the table
// this implements.

// maxEntryText is where an entry's text is cut (contract: 16 KiB).
const maxEntryText = 16 << 10

// maxSummary bounds a tool_use's one-line summary, in runes.
const maxSummary = 200

// record is the part of a JSONL line the mapping reads. toolUseResult, often
// the largest field, is not declared, so it is never kept.
type record struct {
	Type             string          `json:"type"`
	Subtype          string          `json:"subtype"`
	Level            string          `json:"level"`
	Timestamp        string          `json:"timestamp"`
	IsSidechain      bool            `json:"isSidechain"`
	IsMeta           bool            `json:"isMeta"`
	IsCompactSummary bool            `json:"isCompactSummary"`
	IsAPIError       bool            `json:"isApiErrorMessage"`
	Content          json.RawMessage `json:"content"` // system records
	Message          struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	Attachment struct {
		Type        string          `json:"type"`
		Prompt      json.RawMessage `json:"prompt"`
		CommandMode string          `json:"commandMode"`
	} `json:"attachment"`
}

// part is one content block of a message.
type part struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

// mapped is an entry before it has a cursor. toolUseID is set on a tool_use,
// so the index can name the tool_results that answer it.
type mapped struct {
	ports.TranscriptEntry
	toolUseID string
}

// mapLine turns one JSONL line into its entries, in order. toolName names the
// tool of an earlier tool_use by id; it may be nil.
func mapLine(line []byte, toolName func(id string) string) []mapped {
	var r record
	if err := json.Unmarshal(line, &r); err != nil || r.IsSidechain {
		return nil
	}
	at := ""
	if _, err := time.Parse(time.RFC3339Nano, r.Timestamp); err == nil {
		at = r.Timestamp
	}
	var out []mapped
	add := func(kind, text string) {
		if strings.TrimSpace(text) == "" {
			return
		}
		out = append(out, entry(at, kind, text))
	}
	switch r.Type {
	case "user":
		mapUser(&r, at, toolName, add, &out)
	case "assistant":
		mapAssistant(&r, at, add, &out)
	case "system":
		mapSystem(&r, add)
	case "attachment":
		if r.Attachment.Type != "queued_command" {
			return nil
		}
		text := contentText(r.Attachment.Prompt)
		if r.Attachment.CommandMode == "prompt" {
			add(classify(text))
		} else {
			add(ports.TranscriptSystem, notification(text))
		}
	}
	return out
}

func entry(at, kind, text string) mapped {
	text, cut := truncate(text)
	return mapped{TranscriptEntry: ports.TranscriptEntry{At: at, Kind: kind, Text: text, Truncated: cut}}
}

func mapUser(r *record, at string, toolName func(string) string, add func(kind, text string), out *[]mapped) {
	if r.IsCompactSummary {
		return // the compact_boundary system record marks it
	}
	if s, ok := asString(r.Message.Content); ok {
		switch {
		case r.IsMeta && strings.HasPrefix(s, "Stop hook feedback:"):
			add(ports.TranscriptSystem, s)
		case !r.IsMeta:
			add(classify(s))
		}
		return
	}
	if r.IsMeta {
		return
	}
	parts, _ := asParts(r.Message.Content)
	for _, p := range parts {
		switch p.Type {
		case "text":
			add(classify(p.Text))
		case "image":
			add(ports.TranscriptUser, "[image]")
		case "tool_result":
			e := entry(at, ports.TranscriptToolResult, contentText(p.Content))
			e.IsError = p.IsError
			if toolName != nil {
				e.Tool = toolName(p.ToolUseID)
			}
			*out = append(*out, e)
		}
	}
}

func mapAssistant(r *record, at string, add func(kind, text string), out *[]mapped) {
	if r.IsAPIError {
		add(ports.TranscriptSystem, contentText(r.Message.Content))
		return
	}
	if s, ok := asString(r.Message.Content); ok {
		add(ports.TranscriptAssistant, s)
		return
	}
	parts, _ := asParts(r.Message.Content)
	for _, p := range parts {
		switch p.Type {
		case "text":
			add(ports.TranscriptAssistant, p.Text)
		case "tool_use", "server_tool_use":
			e := entry(at, ports.TranscriptToolUse, toolSummary(p.Input))
			e.Tool = p.Name
			e.toolUseID = p.ID
			*out = append(*out, e)
		}
	}
}

func mapSystem(r *record, add func(kind, text string)) {
	if r.Subtype == "compact_boundary" {
		add(ports.TranscriptSystem, "Conversation compacted")
		return
	}
	switch r.Subtype {
	case "local_command", "informational", "away_summary", "scheduled_task_fire", "api_error":
	default:
		if r.Level != "error" {
			return
		}
	}
	s, ok := asString(r.Content)
	if !ok {
		return
	}
	_, text := classify(s)
	add(ports.TranscriptSystem, text)
}

// classify reads what the CLI wraps in a user message: a slash command, its
// output, a background task's notification, an interruption, or what the
// person wrote.
func classify(s string) (kind, text string) {
	t := strings.TrimSpace(s)
	switch {
	case strings.HasPrefix(t, "<command-name>") || strings.HasPrefix(t, "<command-message>"):
		cmd := between(t, "<command-name>", "</command-name>")
		if args := strings.TrimSpace(between(t, "<command-args>", "</command-args>")); args != "" {
			cmd += " " + args
		}
		return ports.TranscriptUser, cmd
	case strings.HasPrefix(t, "<local-command-stdout>"):
		return ports.TranscriptSystem, ansi.Strip(strings.TrimSpace(between(t, "<local-command-stdout>", "</local-command-stdout>")))
	case strings.HasPrefix(t, "<local-command-stderr>"):
		return ports.TranscriptSystem, ansi.Strip(strings.TrimSpace(between(t, "<local-command-stderr>", "</local-command-stderr>")))
	case strings.HasPrefix(t, "<local-command-caveat>"):
		return ports.TranscriptSystem, ""
	case strings.HasPrefix(t, "<task-notification>"):
		return ports.TranscriptSystem, notification(t)
	case strings.HasPrefix(t, "[Request interrupted by user"):
		return ports.TranscriptSystem, t
	}
	return ports.TranscriptUser, s
}

// notification is a task notification's summary, or the whole text when it
// has none.
func notification(s string) string {
	if sum := between(s, "<summary>", "</summary>"); sum != "" {
		return sum
	}
	return s
}

func between(s, open, close string) string {
	_, rest, ok := strings.Cut(s, open)
	if !ok {
		return ""
	}
	in, _, _ := strings.Cut(rest, close)
	return in
}

// asString decodes raw when it is a JSON string. Content is a string or an
// array of blocks; looking at the first byte saves decoding it twice, which
// matters for blocks carrying a base64 image.
func asString(raw json.RawMessage) (string, bool) {
	raw = bytes.TrimLeft(raw, " \t\r\n")
	if len(raw) == 0 || raw[0] != '"' {
		return "", false
	}
	var s string
	return s, json.Unmarshal(raw, &s) == nil
}

// asParts decodes raw when it is an array of content blocks.
func asParts(raw json.RawMessage) ([]part, bool) {
	raw = bytes.TrimLeft(raw, " \t\r\n")
	if len(raw) == 0 || raw[0] != '[' {
		return nil, false
	}
	var parts []part
	return parts, json.Unmarshal(raw, &parts) == nil
}

// contentText is a message or tool_result content as text: a string as is,
// or its text blocks joined, an image as "[image]"; other blocks (thinking,
// tool references, documents) are left out.
func contentText(raw json.RawMessage) string {
	if s, ok := asString(raw); ok {
		return s
	}
	parts, _ := asParts(raw)
	var texts []string
	for _, p := range parts {
		switch p.Type {
		case "text":
			texts = append(texts, p.Text)
		case "image":
			texts = append(texts, "[image]")
		}
	}
	return strings.Join(texts, "\n")
}

// summaryFields are the inputs that say what a tool call does, most telling
// first: Bash's command, the file of Read/Write/Edit, a search's pattern, a
// fetch's url, an agent's description, a path.
var summaryFields = []string{"command", "file_path", "notebook_path", "pattern", "url", "query", "description", "prompt", "skill", "path"}

// toolSummary is a tool call's input on one line.
func toolSummary(input json.RawMessage) string {
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(input, &fields)
	s := ""
	for _, k := range summaryFields {
		var v string
		if json.Unmarshal(fields[k], &v) == nil && v != "" {
			s = v
			if k == "pattern" {
				var p string
				if json.Unmarshal(fields["path"], &p) == nil && p != "" {
					s += " in " + p
				}
			}
			break
		}
	}
	if s == "" {
		var buf bytes.Buffer
		if json.Compact(&buf, input) == nil && buf.String() != "{}" && buf.String() != "null" {
			s = buf.String()
		}
	}
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > maxSummary {
		s = string([]rune(s)[:maxSummary-1]) + "…"
	}
	return s
}

// truncate cuts s to maxEntryText bytes on a rune boundary.
func truncate(s string) (string, bool) {
	if len(s) <= maxEntryText {
		return s, false
	}
	cut := maxEntryText
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut], true
}
