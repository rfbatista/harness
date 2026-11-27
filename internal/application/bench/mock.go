package bench

import (
	"maps"
	"slices"
	"sync"

	"operators-mcp/internal/domain"
)

// MockResult is what a mock tool answers a call with.
type MockResult struct {
	Text    string
	IsError bool
}

// MockLayer stands in for every harness and MCP tool of a bench run (ADR
// 0015): it offers a case's tools, records each call with its arguments, and
// answers with the case's canned result. It touches nothing else.
type MockLayer struct {
	tools []domain.BenchTool
	mu    sync.Mutex
	calls []domain.BenchCall
}

// NewMockLayer returns the layer for a case's mock tools.
func NewMockLayer(tools []domain.BenchTool) *MockLayer {
	return &MockLayer{tools: slices.Clone(tools)}
}

// Tools are the tools the layer offers.
func (m *MockLayer) Tools() []domain.BenchTool { return slices.Clone(m.tools) }

// Call records the call and answers it. A tool the case does not mock is
// recorded too, and answered with an error result.
func (m *MockLayer) Call(tool string, params map[string]any) MockResult {
	m.mu.Lock()
	m.calls = append(m.calls, domain.BenchCall{Tool: tool, Params: maps.Clone(params)})
	m.mu.Unlock()
	for _, t := range m.tools {
		if t.Name == tool {
			return MockResult{Text: t.Result, IsError: t.IsError}
		}
	}
	return MockResult{Text: "tool " + tool + " is not available in this bench case", IsError: true}
}

// Calls are the calls made so far, in order; the slice is the caller's.
func (m *MockLayer) Calls() []domain.BenchCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.calls)
}
