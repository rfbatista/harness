package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestZZMine(t *testing.T) {
	ctx, log := browser(t)
	srv := httptest.NewServer(noStore(http.FileServer(http.Dir(repoRoot))))
	defer srv.Close()
	var res struct {
		Passed, Failed int
		Failures       []struct{ Test, Error string }
	}
	err := chromedp.Run(ctx, chromedp.Navigate(srv.URL+"/web/test/mine.html"), chromedp.Poll(`window.__testResults`, &res, chromedp.WithPollingTimeout(30*time.Second)))
	t.Log(res.Passed, res.Failed, err, log.all())
	for _, f := range res.Failures {
		t.Errorf("%s\n%s", f.Test, f.Error)
	}
}
