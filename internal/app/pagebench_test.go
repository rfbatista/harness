//go:build !windows

package app

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The page benchmark requests every project page of a project several times
// and reads each answer's Server-Timing header, never the log, so it works
// the same against the live server and against the seeded project. It only
// sends GET requests. A page answered without Server-Timing (a server older
// than this feature) is timed by the client, to the first byte, and shown
// with no breakdown.

// benchPage is one project page: its name and its path under the project.
type benchPage struct{ name, path string }

// benchRow is what the benchmark measured for one page.
type benchRow struct {
	page string
	// Median and worst are the page server time, or, with no breakdown, the
	// time to first byte seen by the client.
	median, worst time.Duration
	// The medians of the breakdown; zero with no breakdown.
	db, git, render, frame, catalog time.Duration
	gitCalls, catalogReads          int
	breakdown              bool
}

// pageBench runs the benchmark against the server at base.
type pageBench struct {
	base    string // the server's origin, such as http://127.0.0.1:8080
	project string // the project's id
	runs    int
	client  *http.Client
}

func newPageBench(base, project string, runs int) *pageBench {
	return &pageBench{
		base: strings.TrimSuffix(base, "/"), project: project, runs: runs,
		client: &http.Client{
			Timeout:       time.Minute,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// pages lists the project pages by name. The task page is the project's first
// task; the code page is where /code leads.
func (p *pageBench) pages() ([]benchPage, error) {
	root := "/projects/" + p.project
	board, err := p.get(root)
	if err != nil {
		return nil, err
	}
	task := regexp.MustCompile(`href="(` + regexp.QuoteMeta(root) + `/tasks/[a-z0-9-]+)"`)
	var taskPath string
	for _, m := range task.FindAllStringSubmatch(board.body, -1) {
		if !strings.HasSuffix(m[1], "/tasks/new") {
			taskPath = m[1]
			break
		}
	}
	if taskPath == "" {
		return nil, fmt.Errorf("the board of project %s links to no task, so there is no task page to time", p.project)
	}
	pages := []benchPage{
		{"board", root}, {"task page", taskPath}, {"settings", root + "/settings"}, {"documents", root + "/documents"},
		{"code", root + "/code"}, {"design", root + "/design"}, {"design system", root + "/design-systems"},
		{"usage", root + "/usage"}, {"reviews", root + "/reviews"},
	}
	if path := p.historyPath(board.body, task, root); path != "" {
		pages = append(pages, benchPage{"history", path})
	}
	return pages, nil
}

// historyPath is the first session history page that answers, among the board's
// first tasks' sessions (a session with no worktree has no history page): each
// task page seeds its sessions as JSON. Empty when none does.
func (p *pageBench) historyPath(board string, taskLink *regexp.Regexp, root string) string {
	tried := 0
	for _, t := range taskLink.FindAllStringSubmatch(board, -1) {
		if strings.HasSuffix(t[1], "/tasks/new") {
			continue
		}
		if tried++; tried > 400 {
			break
		}
		page, err := p.get(t[1])
		if err != nil {
			continue
		}
		for _, m := range sessionIDRe.FindAllStringSubmatch(page.body, 5) {
			path := t[1] + "/sessions/" + m[1] + "/history"
			if got, err := p.get(path); err == nil && got.status == http.StatusOK {
				return path
			}
		}
	}
	return ""
}

// sessionIDRe matches a session in the task page's seed.
var sessionIDRe = regexp.MustCompile(`\{"id":"([^"]+)","project_id":"[^"]*","repository_id"`)

// answer is one GET's result.
type answer struct {
	status int
	body   string
	header http.Header
	first  time.Duration // from the request to the first byte of the answer
	where  string        // the Location of a redirect
}

func (p *pageBench) get(path string) (answer, error) {
	req, err := http.NewRequest(http.MethodGet, p.base+path, nil)
	if err != nil {
		return answer{}, err
	}
	var started, first time.Time
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{
		GetConn:              func(string) { started = time.Now() },
		GotFirstResponseByte: func() { first = time.Now() },
	}))
	resp, err := p.client.Do(req)
	if err != nil {
		return answer{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return answer{}, err
	}
	return answer{status: resp.StatusCode, body: string(body), header: resp.Header, first: first.Sub(started), where: resp.Header.Get("Location")}, nil
}

// measure requests page one time to warm up and runs times to measure.
func (p *pageBench) measure(page benchPage) (benchRow, error) {
	path := page.path
	if first, err := p.get(path); err != nil {
		return benchRow{}, fmt.Errorf("%s: %w", page.name, err)
	} else if first.status >= 300 && first.status < 400 {
		path = first.where // a page that leads to another is timed at its end
	}
	var times, dbs, gits, renders, frames, catalogs []time.Duration
	var calls, reads []int
	row := benchRow{page: page.name, breakdown: true}
	for range p.runs {
		a, err := p.get(path)
		if err != nil {
			return benchRow{}, fmt.Errorf("%s: %w", page.name, err)
		}
		if a.status != http.StatusOK {
			return benchRow{}, fmt.Errorf("%s: GET %s answered %d", page.name, path, a.status)
		}
		m, n, nr, ok := parseTiming(a.header.Get("Server-Timing"))
		if !ok {
			row.breakdown = false
			times = append(times, a.first)
			continue
		}
		times = append(times, m["total"])
		dbs, gits, renders, frames, catalogs = append(dbs, m["db"]), append(gits, m["git"]), append(renders, m["render"]), append(frames, m["frame"]), append(catalogs, m["catalog"])
		calls, reads = append(calls, n), append(reads, nr)
	}
	row.median, row.worst = median(times), slices.Max(times)
	if row.breakdown {
		row.db, row.git, row.render, row.frame = median(dbs), median(gits), median(renders), median(frames)
		row.catalog, row.gitCalls, row.catalogReads = median(catalogs), int(median(durations(calls))), int(median(durations(reads)))
	}
	return row, nil
}

// run measures every page of the project.
func (p *pageBench) run() ([]benchRow, error) {
	pages, err := p.pages()
	if err != nil {
		return nil, err
	}
	var rows []benchRow
	for _, page := range pages {
		row, err := p.measure(page)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func median(d []time.Duration) time.Duration {
	s := slices.Clone(d)
	slices.Sort(s)
	return s[len(s)/2]
}

func durations(n []int) []time.Duration {
	out := make([]time.Duration, len(n))
	for i, v := range n {
		out[i] = time.Duration(v)
	}
	return out
}

var timingDur = regexp.MustCompile(`(?:^|;)dur=([0-9.]+)`)
var timingReads = regexp.MustCompile(`desc="(\d+) reads"`)
var timingCalls = regexp.MustCompile(`desc="(\d+) calls"`)

// parseTiming reads a Server-Timing value: each metric's duration, and the
// number of git calls and of catalog reads. It is not ok when there is no total.
func parseTiming(header string) (map[string]time.Duration, int, int, bool) {
	out := map[string]time.Duration{}
	calls, reads := 0, 0
	for _, metric := range strings.Split(header, ",") {
		metric = strings.TrimSpace(metric)
		name, _, _ := strings.Cut(metric, ";")
		if d := timingDur.FindStringSubmatch(metric); d != nil {
			ms, _ := strconv.ParseFloat(d[1], 64)
			out[name] = time.Duration(ms * float64(time.Millisecond))
		}
		if c := timingCalls.FindStringSubmatch(metric); c != nil {
			calls, _ = strconv.Atoi(c[1])
		}
		if c := timingReads.FindStringSubmatch(metric); c != nil {
			reads, _ = strconv.Atoi(c[1])
		}
	}
	_, ok := out["total"]
	return out, calls, reads, ok
}

// table is the rows as the benchmark prints them.
func table(rows []benchRow) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-14s %9s %9s %9s %14s %9s %9s %17s\n", "page", "median", "worst", "db", "git (calls)", "render", "frame", "catalog (reads)")
	ms := func(d time.Duration) string { return fmt.Sprintf("%.0f ms", float64(d)/float64(time.Millisecond)) }
	for _, r := range rows {
		if !r.breakdown {
			fmt.Fprintf(&b, "%-14s %9s %9s   no breakdown (time to first byte)\n", r.page, ms(r.median), ms(r.worst))
			continue
		}
		fmt.Fprintf(&b, "%-14s %9s %9s %9s %14s %9s %9s %17s\n", r.page, ms(r.median), ms(r.worst), ms(r.db), fmt.Sprintf("%s (%d)", ms(r.git), r.gitCalls), ms(r.render), ms(r.frame), fmt.Sprintf("%s (%d)", ms(r.catalog), r.catalogReads))
	}
	return b.String()
}

// projectID is the id of the project named name on the server at base, or
// name itself when it is an id.
func (p *pageBench) projectID(name string) (string, error) {
	list, err := p.get("/projects")
	if err != nil {
		return "", err
	}
	if list.status != http.StatusOK {
		return "", fmt.Errorf("GET /projects answered %d", list.status)
	}
	for _, m := range regexp.MustCompile(`href="/projects/([a-z0-9]+)">([^<]*)</a>`).FindAllStringSubmatch(list.body, -1) {
		if m[2] == name || m[1] == name {
			return m[1], nil
		}
	}
	return "", fmt.Errorf("the server at %s has no project %q", p.base, url.PathEscape(name))
}
