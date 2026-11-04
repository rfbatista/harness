//go:build !windows

package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// A project page reads the agent catalog at most once, however many sessions
// run as agents and however many agents there are: the cost of a read does not
// multiply. The count comes from Server-Timing, so it does not depend on the
// machine.
func TestEveryProjectPageReadsTheAgentCatalogAtMostOnce(t *testing.T) {
	b := newFourTimesProject(t)
	for name, path := range b.projectPages() {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			b.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s: %d", path, rec.Code)
			}
			header := rec.Header().Get("Server-Timing")
			if n := catalogReads(t, header); n > 1 {
				t.Errorf("the %s read the agent catalog %d times, want at most 1: %s", name, n, header)
			}
			dumpPage(t, name, rec.Body.String())
		})
	}
}

// projectPages is every project page's path, by name.
func (b *budgetProject) projectPages() map[string]string {
	base := "/projects/" + b.project
	return map[string]string{
		"board": base, "task page": base + "/tasks/" + b.tickets[0], "settings": base + "/settings",
		"documents": base + "/documents", "code": base + "/repositories/" + b.repo + "/code", "design": base + "/design",
		"design system": base + "/design-systems", "usage": base + "/usage", "reviews": base + "/reviews",
	}
}

var catalogReadsRe = regexp.MustCompile(`catalog;desc="(\d+) reads?"`)

// catalogReads is the read count a Server-Timing header reports for the catalog.
func catalogReads(t *testing.T, header string) int {
	t.Helper()
	m := catalogReadsRe.FindStringSubmatch(header)
	if m == nil {
		t.Fatalf("Server-Timing %q has no catalog read count", header)
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// dumpPage writes a page's body into $PAGE_DUMP_DIR when it is set, so the
// pages of two builds can be compared by hand: a change of speed must not
// change what a page shows.
func dumpPage(t *testing.T, name, body string) {
	t.Helper()
	dir := os.Getenv("PAGE_DUMP_DIR")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, strings.ReplaceAll(name, " ", "-")+".html"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
