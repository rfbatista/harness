//go:build !windows

package web_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/chromedp"
)

// refuseSkillWrites answers the page's save_catalog_skill and
// delete_catalog_skill with the coded refusal. The page never sends such a
// write for a built-in skill, so a test that shows how it reads one stages the
// answer in the browser; every other request goes to the real server.
func refuseSkillWrites(t *testing.T, b *browser, status int64, code, message string) {
	t.Helper()
	chromedp.ListenTarget(b.ctx, func(ev any) {
		e, ok := ev.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			ctx := cdp.WithExecutor(context.Background(), chromedp.FromContext(b.ctx).Target)
			body, _ := json.Marshal(map[string]any{"error": message, "code": code})
			_ = fetch.FulfillRequest(e.RequestID, status).
				WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "application/json"}}).
				WithBody(base64.StdEncoding.EncodeToString(body)).Do(ctx)
		}()
	})
	patterns := []*fetch.RequestPattern{
		{URLPattern: "*/api/save_catalog_skill*", RequestStage: fetch.RequestStageRequest},
		{URLPattern: "*/api/delete_catalog_skill*", RequestStage: fetch.RequestStageRequest},
	}
	if err := chromedp.Run(b.ctx, fetch.Enable().WithPatterns(patterns)); err != nil {
		t.Fatal(err)
	}
}
