package domain

import (
	"bytes"
	"encoding/json"
	"strings"
)

// SettingAgentAttribution is the settings key holding the agent attribution
// (ADR 0063): the credit line claude adds to the commits and the pull
// requests an agent writes. Unset means Claude default for both.
const SettingAgentAttribution = "agent_attribution"

// AttributionMode says what one attribution line is. The zero value is
// Claude default.
type AttributionMode string

const (
	// AttributionDefault leaves claude's own line (it names the model).
	AttributionDefault AttributionMode = ""
	// AttributionNone removes the line.
	AttributionNone AttributionMode = "none"
	// AttributionCustom replaces the line with Text.
	AttributionCustom AttributionMode = "custom"
)

// AttributionChoice is one line's mode, and its text when the mode is custom.
type AttributionChoice struct {
	Mode AttributionMode `json:"mode"`
	Text string          `json:"text,omitempty"`
}

// AgentAttribution is the person's choice for the commit trailer and the
// pull request line. The zero value is Claude default for both.
type AgentAttribution struct {
	Commit AttributionChoice `json:"commit"`
	PR     AttributionChoice `json:"pr"`
}

// Encode is the stored form.
func (a AgentAttribution) Encode() string {
	b, _ := json.Marshal(a)
	return string(b)
}

// ParseAgentAttribution reads the stored form; an unset or unreadable value
// is Claude default.
func ParseAgentAttribution(raw string) AgentAttribution {
	var a AgentAttribution
	if raw != "" && json.Unmarshal([]byte(raw), &a) != nil {
		return AgentAttribution{}
	}
	return a.normalized()
}

func (c AttributionChoice) normalized() AttributionChoice {
	switch c.Mode {
	case AttributionNone:
		return AttributionChoice{Mode: AttributionNone}
	case AttributionCustom:
		return AttributionChoice{Mode: AttributionCustom, Text: strings.TrimSpace(c.Text)}
	}
	return AttributionChoice{}
}

func (a AgentAttribution) normalized() AgentAttribution {
	return AgentAttribution{Commit: a.Commit.normalized(), PR: a.PR.normalized()}
}

// Normalize trims the custom texts. It refuses a custom text that is empty
// once trimmed, because None already means no line.
func (a AgentAttribution) Normalize() (AgentAttribution, error) {
	for _, line := range []struct {
		name   string
		choice AttributionChoice
	}{{"Commits", a.Commit}, {"Pull requests", a.PR}} {
		name, c := line.name, line.choice
		switch c.Mode {
		case AttributionDefault, AttributionNone:
		case AttributionCustom:
			if strings.TrimSpace(c.Text) == "" {
				return AgentAttribution{}, &StructuredError{Code: "INVALID_INPUT", Message: name + ": enter the custom text, or choose None to leave the line out."}
			}
		default:
			return AgentAttribution{}, &StructuredError{Code: "INVALID_INPUT", Message: name + ": the mode must be Claude default, None or Custom text."}
		}
	}
	return a.normalized(), nil
}

// value is the string claude gets for the choice, and false when the key is
// left out so that claude writes its own line.
func (c AttributionChoice) value() (string, bool) {
	switch c.Mode {
	case AttributionNone:
		return "", true
	case AttributionCustom:
		return c.Text, true
	}
	return "", false
}

// Launch is the "attribution" settings value that an agent's claude gets in
// its --settings: a commit and/or pr string, each only for a choice that is
// not Claude default. Nil when both are, so the key is left out. The Agent
// isolation page's preview is this same rule.
func (a AgentAttribution) Launch() json.RawMessage {
	out := map[string]string{}
	if v, ok := a.Commit.value(); ok {
		out["commit"] = v
	}
	if v, ok := a.PR.value(); ok {
		out["pr"] = v
	}
	if len(out) == 0 {
		return nil
	}
	// Not HTML-escaped, so that the page's preview, built in the browser,
	// reads the same.
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(out)
	return json.RawMessage(strings.TrimSuffix(b.String(), "\n"))
}

// Preview is the JSON the page shows: Launch under its key, with a "{}" for
// the Claude default on both.
func (a AgentAttribution) Preview() string {
	l := a.Launch()
	if l == nil {
		return "{}"
	}
	return `{"attribution":` + string(l) + `}`
}
