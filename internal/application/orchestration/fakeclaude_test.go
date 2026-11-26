package orchestration

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeLaunchFile is where the fake CLI, run with CLAUDE_FAKE_RECORD=1, writes
// how it was launched, in its working directory: what a headless session's
// CLI gets.
const fakeLaunchFile = "fake-claude-launch.json"

// fakeLaunch is what fakeLaunchFile holds.
type fakeLaunch struct {
	Args     []string `json:"args"`
	Shell    string   `json:"shell"`     // HARNESS_SHELL
	ShellURL string   `json:"shell_url"` // HARNESS_SHELL_URL
	Path     string   `json:"path"`      // PATH
	// Env holds the variables of fakeLaunchEnv the CLI was given.
	Env map[string]string `json:"env"`
	// Resolved is where the CLI's own PATH finds rtk, tee and sed, as
	// `command -v` would: a real lookup in the process that was started.
	Resolved map[string]string `json:"resolved"`
}

// fakeLaunchEnv are the variables fakeLaunch.Env records.
var fakeLaunchEnv = []string{"RTK_TELEMETRY_DISABLED", "DO_NOT_TRACK", "CODEGRAPH_TELEMETRY", "ANTHROPIC_BASE_URL", "PATH", "CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_API_KEY", "CLAUDE_CONFIG_DIR", "NODE_OPTIONS", "HARNESS_TEST_WORKERS", "VITEST_MAX_WORKERS", "VITEST_MAX_THREADS", "VITEST_MAX_FORKS"}

func TestMain(m *testing.M) {
	if os.Getenv("CLAUDE_FAKE") == "1" {
		runFakeClaude()
		return
	}
	os.Exit(m.Run())
}

func runFakeClaude() {
	if os.Getenv("CLAUDE_FAKE_RECORD") == "1" {
		env := map[string]string{}
		for _, name := range fakeLaunchEnv {
			if v, ok := os.LookupEnv(name); ok {
				env[name] = v
			}
		}
		resolved := map[string]string{}
		for _, name := range []string{"rtk", "tee", "sed"} {
			if p, err := exec.LookPath(name); err == nil {
				resolved[name] = p
			}
		}
		b, _ := json.Marshal(fakeLaunch{Args: os.Args[1:], Shell: os.Getenv("HARNESS_SHELL"),
			ShellURL: os.Getenv("HARNESS_SHELL_URL"), Path: os.Getenv("PATH"), Env: env, Resolved: resolved})
		_ = os.WriteFile(fakeLaunchFile+".tmp", b, 0o644)
		_ = os.Rename(fakeLaunchFile+".tmp", fakeLaunchFile)
	}
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	script := newFakeScript()
	if init := os.Getenv("CLAUDE_FAKE_INIT"); init != "" {
		fmt.Fprintln(out, init)
	} else {
		fmt.Fprintln(out, `{"type":"system","subtype":"init","session_id":"fake"}`)
	}
	out.Flush()
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		if sc.Text() == "" {
			continue
		}
		script.play()
		fmt.Fprintln(out, `{"type":"assistant","session_id":"fake","message":{"role":"assistant","content":[{"type":"text","text":"working"}]}}`)
		fmt.Fprintln(out, `{"type":"result","subtype":"success","session_id":"fake","result":"ok","total_cost_usd":0.02,"usage":{"input_tokens":5,"output_tokens":9}}`)
		out.Flush()
	}
	os.Exit(0)
}

// fakeScriptEnv holds the calls a bench run's fake CLI makes through the MCP
// server it is given (a JSON list of {tool, params}); fakeBenchRecordEnv names
// the file it writes how it was launched to. Both unset: the fake makes no
// call and records nothing.
const (
	fakeScriptEnv      = "CLAUDE_FAKE_SCRIPT"
	fakeBenchRecordEnv = "CLAUDE_FAKE_BENCH_RECORD"
)

// fakeBenchLaunch is what the scripted fake CLI recorded: its arguments, its
// working directory and the files in it, and the MCP configuration it was
// given.
type fakeBenchLaunch struct {
	Args      []string `json:"args"`
	Cwd       string   `json:"cwd"`
	Files     []string `json:"files"`
	MCPConfig string   `json:"mcp_config"`
	Servers   []string `json:"servers"`
	Answers   []string `json:"answers"`
}

type fakeScript struct {
	steps []struct {
		Tool   string         `json:"tool"`
		Params map[string]any `json:"params"`
	}
	launch fakeBenchLaunch
	urls   map[string]string
}

// newFakeScript reads the script and the MCP configuration the CLI was
// launched with, as the real CLI reads it at startup.
func newFakeScript() *fakeScript {
	fs := &fakeScript{urls: map[string]string{}}
	if raw := os.Getenv(fakeScriptEnv); raw != "" {
		_ = json.Unmarshal([]byte(raw), &fs.steps)
	}
	if os.Getenv(fakeBenchRecordEnv) == "" {
		return fs
	}
	fs.launch.Args = os.Args[1:]
	fs.launch.Cwd, _ = os.Getwd()
	for i, a := range os.Args {
		if a == "--mcp-config" && i+1 < len(os.Args) {
			b, _ := os.ReadFile(os.Args[i+1])
			fs.launch.MCPConfig = string(b)
		}
	}
	var cfg struct {
		Servers map[string]struct {
			URL string `json:"url"`
		} `json:"mcpServers"`
	}
	_ = json.Unmarshal([]byte(fs.launch.MCPConfig), &cfg)
	for name, srv := range cfg.Servers {
		fs.launch.Servers = append(fs.launch.Servers, name)
		fs.urls[name] = srv.URL
	}
	_ = filepath.WalkDir(fs.launch.Cwd, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(fs.launch.Cwd, p)
			fs.launch.Files = append(fs.launch.Files, rel)
		}
		return nil
	})
	return fs
}

// play makes the scripted calls on the "bench" MCP server and records the
// launch and what the server answered.
func (fs *fakeScript) play() {
	url := fs.urls["bench"]
	for i, step := range fs.steps {
		if url == "" {
			fs.launch.Answers = append(fs.launch.Answers, "no bench server")
			continue
		}
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": i + 1, "method": "tools/call",
			"params": map[string]any{"name": step.Tool, "arguments": step.Params}})
		req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			fs.launch.Answers = append(fs.launch.Answers, "error: "+err.Error())
			continue
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		fs.launch.Answers = append(fs.launch.Answers, string(raw))
	}
	if path := os.Getenv(fakeBenchRecordEnv); path != "" {
		b, _ := json.Marshal(fs.launch)
		_ = os.WriteFile(path, b, 0o644)
	}
}
