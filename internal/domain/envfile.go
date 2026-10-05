package domain

import (
	"path"
	"strings"
	"time"
)

// EnvFile is a file of environment variables a repository's sessions need but
// git does not carry, such as .env: the harness keeps it and writes it into
// every session worktree cut from the repository. Path is relative to the
// checkout.
type EnvFile struct {
	RepositoryID string    `json:"repository_id"`
	Path         string    `json:"path"`
	Content      string    `json:"content"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// MaxEnvFileSize bounds an env file: they hold variables, not data.
const MaxEnvFileSize = 256 << 10

// DefaultEnvFilePath is the env file a repository starts with.
const DefaultEnvFilePath = ".env"

// CleanEnvFilePath validates an env file's path and returns it cleaned: it
// must stay inside the checkout (relative, no ..) and outside .git.
func CleanEnvFilePath(p string) (string, error) {
	p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
	invalid := func(why string) error {
		return &StructuredError{Code: "INVALID_PATH", Message: "env file path " + why}
	}
	if p == "" {
		return "", invalid("is required")
	}
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, "~") {
		return "", invalid("must be relative to the repository, like .env or apps/api/.env")
	}
	clean := path.Clean(p)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", invalid("must stay inside the repository")
	}
	if clean == ".git" || strings.HasPrefix(clean, ".git/") {
		return "", invalid("must not be inside .git")
	}
	return clean, nil
}
