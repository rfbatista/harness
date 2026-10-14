package claudetext

import (
	"context"

	"github.com/rfbatista/llmkit/claude"
)

// fakeOneShot stands in for llmkit's one-shot mode: it remembers the
// request and answers with result (Text defaults to "done") or err.
type fakeOneShot struct {
	got    []claude.OneShotRequest
	result claude.OneShotResult
	err    error
}

func (f *fakeOneShot) OneShot(ctx context.Context, req claude.OneShotRequest) (claude.OneShotResult, error) {
	f.got = append(f.got, req)
	if err := ctx.Err(); err != nil {
		return claude.OneShotResult{}, err
	}
	if f.err != nil {
		return claude.OneShotResult{}, f.err
	}
	if f.result.Text == "" {
		f.result.Text = "done"
	}
	return f.result, nil
}

func claudeResult(text string) claude.OneShotResult { return claude.OneShotResult{Text: text} }
