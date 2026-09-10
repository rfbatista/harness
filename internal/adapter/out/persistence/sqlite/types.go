package sqlite

import (
	"database/sql/driver"
	"encoding/json"

	"operators-mcp/internal/domain"
)

// stringSlice is a []string that scans from JSON or a single plain string.
// This avoids "invalid character '.' looking for beginning of value" when
// the column contains a path like ".git" or empty/invalid JSON.
type stringSlice []string

// Scan implements sql.Scanner. Accepts nil, empty string, valid JSON array,
// or a single plain string (e.g. ".git") and normalizes to []string.
func (s *stringSlice) Scan(value interface{}) error {
	if value == nil {
		*s = nil
		return nil
	}
	var b []byte
	switch v := value.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		*s = nil
		return nil
	}
	if len(b) == 0 {
		*s = []string{}
		return nil
	}
	// Trim space; empty after trim -> empty slice
	if len(b) > 0 && (b[0] == '[' || b[0] == '"' || b[0] == '{') {
		// Looks like JSON: unmarshal
		var out []string
		if err := json.Unmarshal(b, &out); err != nil {
			// Invalid JSON: treat whole value as single path
			*s = []string{string(b)}
			return nil
		}
		*s = out
		return nil
	}
	// Plain string (e.g. ".git" or "node_modules")
	*s = []string{string(b)}
	return nil
}

// Value implements driver.Valuer. Always returns valid JSON.
func (s stringSlice) Value() (driver.Value, error) {
	if s == nil {
		return "[]", nil
	}
	return json.Marshal(s)
}

// stringMap is a map[string]string that scans from JSON.
type stringMap map[string]string

// Scan implements sql.Scanner.
func (m *stringMap) Scan(value interface{}) error {
	if value == nil {
		*m = nil
		return nil
	}
	var b []byte
	switch v := value.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		*m = nil
		return nil
	}
	if len(b) == 0 || string(b) == "{}" {
		*m = map[string]string{}
		return nil
	}
	var out map[string]string
	if err := json.Unmarshal(b, &out); err != nil {
		*m = map[string]string{}
		return nil
	}
	*m = out
	return nil
}

// Value implements driver.Valuer.
func (m stringMap) Value() (driver.Value, error) {
	if m == nil {
		return "{}", nil
	}
	return json.Marshal(m)
}

// agentSlice is a []domain.Agent that scans from JSON or invalid/empty content.
// Uses the same defensive logic as stringSlice so malformed DB values don't break reads.
type agentSlice []domain.Agent

// Scan implements sql.Scanner.
func (a *agentSlice) Scan(value interface{}) error {
	if value == nil {
		*a = nil
		return nil
	}
	var b []byte
	switch v := value.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		*a = nil
		return nil
	}
	if len(b) == 0 {
		*a = []domain.Agent{}
		return nil
	}
	if b[0] != '[' {
		*a = []domain.Agent{}
		return nil
	}
	var out []domain.Agent
	if err := json.Unmarshal(b, &out); err != nil {
		*a = []domain.Agent{}
		return nil
	}
	*a = out
	return nil
}

// Value implements driver.Valuer.
func (a agentSlice) Value() (driver.Value, error) {
	if a == nil {
		return "[]", nil
	}
	return json.Marshal(a)
}

// jsonMap is a map[string]any that scans from JSON. Used for Tool.InputSchema.
type jsonMap map[string]any

// Scan implements sql.Scanner.
func (m *jsonMap) Scan(value interface{}) error {
	if value == nil {
		*m = nil
		return nil
	}
	var b []byte
	switch v := value.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		*m = nil
		return nil
	}
	if len(b) == 0 || string(b) == "{}" {
		*m = map[string]any{}
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		*m = map[string]any{}
		return nil
	}
	*m = out
	return nil
}

// Value implements driver.Valuer.
func (m jsonMap) Value() (driver.Value, error) {
	if m == nil {
		return "{}", nil
	}
	return json.Marshal(m)
}

// languageTermSlice is a []domain.LanguageTerm that scans from JSON.
type languageTermSlice []domain.LanguageTerm

// Scan implements sql.Scanner.
func (l *languageTermSlice) Scan(value interface{}) error {
	if value == nil {
		*l = nil
		return nil
	}
	var b []byte
	switch v := value.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		*l = nil
		return nil
	}
	if len(b) == 0 || b[0] != '[' {
		*l = []domain.LanguageTerm{}
		return nil
	}
	var out []domain.LanguageTerm
	if err := json.Unmarshal(b, &out); err != nil {
		*l = []domain.LanguageTerm{}
		return nil
	}
	*l = out
	return nil
}

// Value implements driver.Valuer.
func (l languageTermSlice) Value() (driver.Value, error) {
	if l == nil {
		return "[]", nil
	}
	return json.Marshal(l)
}

// promptSlice is a []domain.Prompt that scans from JSON.
// Stores only {id, name} refs; full Prompt is resolved at the service layer.
type promptSlice []domain.Prompt

// Scan implements sql.Scanner.
func (p *promptSlice) Scan(value interface{}) error {
	if value == nil {
		*p = nil
		return nil
	}
	var b []byte
	switch v := value.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		*p = nil
		return nil
	}
	if len(b) == 0 {
		*p = []domain.Prompt{}
		return nil
	}
	if b[0] != '[' {
		*p = []domain.Prompt{}
		return nil
	}
	var out []domain.Prompt
	if err := json.Unmarshal(b, &out); err != nil {
		*p = []domain.Prompt{}
		return nil
	}
	*p = out
	return nil
}

// Value implements driver.Valuer.
func (p promptSlice) Value() (driver.Value, error) {
	if p == nil {
		return "[]", nil
	}
	return json.Marshal(p)
}
