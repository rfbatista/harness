package orchestration

import (
	"encoding/json"
	"strings"
	"sync"
)

// pendingAsk is what one outstanding approval asks for, as one line.
type pendingAsk struct {
	reqID string
	text  string
}

// asks holds what each session's outstanding approvals ask for, oldest first,
// so the Agent board can say what a session waits on. It lives in memory: an
// approval does not outlive its process.
type asks struct {
	mu        sync.Mutex
	bySession map[string][]pendingAsk
}

func (a *asks) add(sessionID, reqID, text string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.bySession == nil {
		a.bySession = map[string][]pendingAsk{}
	}
	a.bySession[sessionID] = append(a.bySession[sessionID], pendingAsk{reqID: reqID, text: text})
}

func (a *asks) drop(sessionID, reqID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	list := a.bySession[sessionID]
	for i, p := range list {
		if p.reqID == reqID {
			list = append(list[:i:i], list[i+1:]...)
			break
		}
	}
	if len(list) == 0 {
		delete(a.bySession, sessionID)
		return
	}
	a.bySession[sessionID] = list
}

func (a *asks) clear(sessionID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.bySession, sessionID)
}

// PendingAsk is what the session's oldest outstanding approval asks for, in
// one line; "" when it has none or the server cannot say.
func (s *Service) PendingAsk(sessionID string) string {
	s.asks.mu.Lock()
	defer s.asks.mu.Unlock()
	if list := s.asks.bySession[sessionID]; len(list) > 0 {
		return list[0].text
	}
	return ""
}

// askLine renders an approval request as one line: the question of an
// AskUserQuestion, or the tool with the part of its input that says what it
// will do.
func askLine(toolName string, input json.RawMessage) string {
	if qs := ParseQuestions(toolName, input); len(qs) > 0 && qs[0].Question != "" {
		return oneLine(qs[0].Question)
	}
	var fields map[string]any
	if json.Unmarshal(input, &fields) == nil {
		for _, key := range []string{"command", "file_path", "path", "url", "pattern", "query", "description"} {
			if v, ok := fields[key].(string); ok && strings.TrimSpace(v) != "" {
				return oneLine(toolName + ": " + v)
			}
		}
	}
	return oneLine(toolName)
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
