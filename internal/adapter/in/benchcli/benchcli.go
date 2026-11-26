// Package benchcli is the harness CLI's bench command (ADR 0015): it starts a
// bench run of a project on a running server through the HTTP API, waits for
// it to end and prints its scorecard beside the previous run's.
//
//	server bench run -project <id> [-url http://127.0.0.1:8080] [-token <t>]
//	                 [-agent <id>] [-repository <id>] [-model <m>]
//
// The exit code is 0 when the run finished, 1 when it did not (the server's
// message is on stderr) and 2 for a command that is wrong. Ctrl-C cancels the
// run on the server before the command exits.
package benchcli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"operators-mcp/internal/domain"
)

// Main runs the command with the process's arguments after "bench" and
// environment, and answers the exit code.
func Main(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return Run(ctx, args, os.Stdout, os.Stderr)
}

// Run runs the command: args are those after "bench".
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "run" {
		fmt.Fprintln(stderr, "usage: bench run -project <id> [-url <server>] [-token <token>] [-agent <id>] [-repository <id>] [-model <model>]")
		return 2
	}
	fs := flag.NewFlagSet("bench run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	project := fs.String("project", "", "the project to run the bench of")
	url := fs.String("url", envOr("HARNESS_URL", "http://127.0.0.1:8080"), "the running server")
	token := fs.String("token", os.Getenv("CODING_POOL_TOKEN"), "the server's bearer token, when it requires one")
	agent := fs.String("agent", "", "the agent the run is set up as (default: plain claude)")
	repository := fs.String("repository", "", "the repository whose instruction files the run gets (default: the project's first)")
	model := fs.String("model", "", "the model the run's sessions use (default: the model sessions run on)")
	poll := fs.Duration("poll", time.Second, "how often the run is asked how far it is")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if *project == "" {
		fmt.Fprintln(stderr, "bench run: -project is required")
		return 2
	}
	c := client{base: strings.TrimRight(*url, "/") + "/api/projects/" + *project + "/bench", token: *token}

	var run domain.BenchRun
	if err := c.do(ctx, http.MethodPost, "/runs", domain.BenchRunRequest{AgentID: *agent, RepositoryID: *repository, Model: *model}, &run); err != nil {
		fmt.Fprintln(stderr, "bench run:", err)
		return 1
	}
	fmt.Fprintf(stdout, "bench run %s started: %d cases\n", run.ID, run.Total)
	for run.Status == domain.BenchRunRunning {
		select {
		case <-ctx.Done():
			return interrupted(ctx, c, run.ID, stderr)
		case <-time.After(*poll):
		}
		if err := c.do(ctx, http.MethodGet, "/runs/"+run.ID, nil, &run); err != nil {
			if ctx.Err() != nil {
				return interrupted(ctx, c, run.ID, stderr)
			}
			fmt.Fprintln(stderr, "bench run:", err)
			return 1
		}
	}
	if run.ScorecardID == "" {
		fmt.Fprintf(stderr, "bench run: the run %s: %s\n", run.Status, run.Error)
		return 1
	}
	var card domain.Scorecard
	if err := c.do(ctx, http.MethodGet, "/scorecards/"+run.ScorecardID, nil, &card); err != nil {
		fmt.Fprintln(stderr, "bench run:", err)
		return 1
	}
	printScorecard(stdout, card)

	var cmp domain.ScorecardComparison
	switch err := c.do(ctx, http.MethodGet, "/scorecards/"+run.ScorecardID+"/compare", nil, &cmp); {
	case err == nil:
		printComparison(stdout, cmp)
	case isCode(err, "BENCH_NO_PREVIOUS_SCORECARD"):
		fmt.Fprintln(stdout, "no previous run to compare with")
	default:
		fmt.Fprintln(stderr, "bench run:", err)
		return 1
	}
	if run.Status != domain.BenchRunFinished {
		fmt.Fprintf(stderr, "bench run: the run %s\n", run.Status)
		return 1
	}
	return 0
}

// interrupted cancels the run on the server after the command's context ended
// (Ctrl-C), with a context of its own, and answers the exit code.
func interrupted(ctx context.Context, c client, runID string, stderr io.Writer) int {
	stop, release := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer release()
	var run domain.BenchRun
	if err := c.do(stop, http.MethodPost, "/runs/"+runID+"/cancel", nil, &run); err != nil {
		fmt.Fprintln(stderr, "bench run: interrupted; the run could not be cancelled:", err)
	} else {
		fmt.Fprintf(stderr, "bench run: interrupted; run %s cancelled\n", runID)
	}
	return 1
}

func envOr(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func printScorecard(w io.Writer, card domain.Scorecard) {
	s := card.Setup
	fmt.Fprintf(w, "\nScorecard %s (%s)\n", card.ID, card.Status)
	fmt.Fprintf(w, "setup: agent %q, model %q, judge %q, cli %q, fingerprint %.12s\n", s.AgentName, s.Model, s.JudgeModel, s.CLIVersion, s.Fingerprint)
	for _, c := range card.Cases {
		name := c.CaseName
		if name == "" {
			name = c.CaseID
		}
		fmt.Fprintf(w, "  %s:", name)
		for _, p := range domain.BenchPatterns {
			if ps := c.Pattern(p); ps.Scored {
				fmt.Fprintf(w, " %s %.2f", p, ps.Score)
			}
		}
		if c.Error != "" {
			fmt.Fprintf(w, " (did not run: %s)", c.Error)
		}
		fmt.Fprintln(w)
	}
	for _, p := range domain.BenchPatterns {
		if a, ok := card.Patterns[p]; ok {
			fmt.Fprintf(w, "  %s: mean %.2f over %d cases\n", p, a.Mean, a.Scored)
		}
	}
	fmt.Fprintf(w, "aggregate %.2f (%d cases scored), cost $%.4f, duration %s\n", card.Aggregate.Mean, card.Aggregate.Scored, card.CostUSD, time.Duration(card.DurationMS)*time.Millisecond)
}

func printComparison(w io.Writer, cmp domain.ScorecardComparison) {
	setup := "setup unchanged"
	if cmp.FingerprintChanged {
		setup = "setup CHANGED"
	}
	fmt.Fprintf(w, "\ncompared with previous run %s (%s): aggregate %.2f -> %.2f (%+.2f)\n", cmp.PreviousID, setup, cmp.Aggregate.Before, cmp.Aggregate.After, cmp.Aggregate.Change)
	for _, c := range cmp.Cases {
		name := c.CaseName
		if name == "" {
			name = c.CaseID
		}
		fmt.Fprintf(w, "  %s: %s", name, c.Status)
		for _, p := range domain.BenchPatterns {
			if d, ok := c.Patterns[p]; ok {
				fmt.Fprintf(w, " %s %.2f -> %.2f (%+.2f)", p, d.Before, d.After, d.Change)
			}
		}
		fmt.Fprintln(w)
	}
}

type client struct{ base, token string }

// apiError is what the server answered with when it refused.
type apiError struct {
	Status  int
	Code    string
	Message string
}

func (e *apiError) Error() string { return e.Message }

func isCode(err error, code string) bool {
	e, ok := err.(*apiError)
	return ok && e.Code == code
}

func (c client) do(ctx context.Context, method, path string, body, into any) error {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("the server is not reachable: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		var e struct{ Error, Code string }
		if json.Unmarshal(raw, &e) != nil || e.Error == "" {
			e.Error = fmt.Sprintf("%s %s: %s", method, path, resp.Status)
		}
		return &apiError{Status: resp.StatusCode, Code: e.Code, Message: e.Error}
	}
	return json.Unmarshal(raw, into)
}
