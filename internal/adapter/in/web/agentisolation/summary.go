package agentisolation

// Chip is one summary chip: the state of a section in a few words, linking
// to the section. The sections announce the same chips on the page as they
// change (see isolationSummary.js), so the words here and there agree.
type Chip struct {
	Text string `json:"text"`
	// Tone is "signal", "attention", "danger" or "" for neutral.
	Tone string `json:"tone"`
	// Blocks says agents cannot start until it is fixed.
	Blocks bool `json:"blocks"`
}

// Summary is the row of chips at the top of the page.
type Summary struct {
	Credential Chip `json:"credential"`
	Claude     Chip `json:"claude"`
	Sandbox    Chip `json:"sandbox"`
}

// NewSummary reads the chips off the three sections' views.
func NewSummary(c CredentialView, cl ClaudeView, s SandboxView) Summary {
	return Summary{Credential: c.Summary(), Claude: cl.Summary(), Sandbox: s.Summary()}
}

// Blocked says something stops agents from starting.
func (s Summary) Blocked() bool { return s.Credential.Blocks || s.Claude.Blocks || s.Sandbox.Blocks }

// Summary is the credential's chip.
func (v CredentialView) Summary() Chip {
	if v.Set() {
		return Chip{Text: "Credential: set", Tone: "signal"}
	}
	return Chip{Text: "Credential: missing", Tone: "danger", Blocks: true}
}

// Summary is the claude's chip.
func (v ClaudeView) Summary() Chip {
	switch {
	case !v.None():
		return Chip{Text: "claude: " + v.State.Current, Tone: "signal"}
	case v.Overridden:
		return Chip{Text: "claude: override", Tone: "signal"}
	case v.Installing():
		return Chip{Text: "claude: installing", Blocks: true}
	}
	return Chip{Text: "claude: not installed", Tone: "danger", Blocks: true}
}

// Summary is the sandbox's chip.
func (v SandboxView) Summary() Chip {
	switch {
	case v.Off():
		return Chip{Text: "Sandbox: off", Tone: "attention"}
	case v.NoDriver():
		return Chip{Text: "Sandbox: no driver", Tone: "danger", Blocks: true}
	}
	return Chip{Text: "Sandbox: on", Tone: "signal"}
}
