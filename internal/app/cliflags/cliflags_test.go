package cliflags

import (
	"flag"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestParseDefaults(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	cfg, err := Parse(fs, nil, env(nil))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.HTTPAddr != ":8080" || cfg.MCPAddr != ":8081" || cfg.DBPath != "data.db" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.ClaudeBin != "claude" || !cfg.ClaudeLogStdout || cfg.ApprovalTimeout != 24*time.Hour {
		t.Fatalf("unexpected claude defaults: %+v", cfg)
	}
	if cfg.Root == "" {
		t.Fatalf("root should default to the working directory")
	}
}

func TestParseEnvFallback(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	cfg, err := Parse(fs, nil, env(map[string]string{
		"HTTP_ADDR":         ":9090",
		"CLAUDE_LOG_STDOUT": "false",
		"APPROVAL_TIMEOUT":  "90m",
		"PROJECT_ROOT":      "/srv/root",
	}))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.HTTPAddr != ":9090" || cfg.ClaudeLogStdout || cfg.ApprovalTimeout != 90*time.Minute || cfg.Root != "/srv/root" {
		t.Fatalf("env not applied: %+v", cfg)
	}
}

func TestParseFlagsOverrideEnv(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	cfg, err := Parse(fs, []string{"--http.addr=:7070", "--db=:memory:"}, env(map[string]string{"HTTP_ADDR": ":9090"}))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.HTTPAddr != ":7070" || cfg.DBPath != ":memory:" {
		t.Fatalf("flags should win: %+v", cfg)
	}
}

func TestParseDotfilesAgentsDirFromEnv(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	cfg, err := Parse(fs, nil, env(map[string]string{"DOTFILES_AGENTS_DIR": "/Users/me/dotfiles/agents"}))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.DotfilesAgentsDir != "/Users/me/dotfiles/agents" {
		t.Fatalf("env not applied: %+v", cfg)
	}
}

func TestParseDotfilesAgentsDirDefaultsEmpty(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	cfg, err := Parse(fs, nil, env(nil))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.DotfilesAgentsDir != "" {
		t.Fatalf("expected empty default, got %q", cfg.DotfilesAgentsDir)
	}
}

func TestParseBadEnvFallsBack(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	cfg, err := Parse(fs, nil, env(map[string]string{"APPROVAL_TIMEOUT": "soon", "CLAUDE_LOG_STDOUT": "maybe"}))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.ApprovalTimeout != 24*time.Hour || !cfg.ClaudeLogStdout {
		t.Fatalf("unparseable env should fall back: %+v", cfg)
	}
}
