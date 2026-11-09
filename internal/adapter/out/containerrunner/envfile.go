package containerrunner

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// EnvFilePath is the file of c's variables for the run named name, in the
// service's harness-owned folder. It exists only while the runtime command
// starts.
func EnvFilePath(name string, c ports.ServiceContainer) string {
	return filepath.Join(c.EnvDir, name+".env")
}

// WriteEnvFile writes c's variables as raw KEY=VALUE lines (what
// `--env-file` reads: no quoting, no escapes) to EnvFilePath, a file only the
// harness's user can read in a folder only it can enter. A later entry of a
// key wins. A value with a line break cannot be one line of the file, so it
// is left out and the log names its key. remove deletes the file and may be
// called again; a container with no variables has no file.
func WriteEnvFile(name string, c ports.ServiceContainer) (remove func(), err error) {
	if len(c.Env) == 0 {
		return func() {}, nil
	}
	var keys []string
	vals := map[string]string{}
	for _, kv := range c.Env {
		k, v, _ := strings.Cut(kv, "=")
		if strings.ContainsAny(v, "\n\r\x00") {
			slog.Warn("a service env value holds a line break and is left out of the container's env", "key", k)
			delete(vals, k)
			continue
		}
		if _, seen := vals[k]; !seen {
			keys = append(keys, k)
		}
		vals[k] = v
	}
	var b strings.Builder
	written := map[string]bool{}
	for _, k := range keys {
		if v, ok := vals[k]; ok && !written[k] && domain.ValidEnvKey(k) {
			written[k] = true
			b.WriteString(k + "=" + v + "\n")
		}
	}
	if err := os.MkdirAll(c.EnvDir, 0o700); err != nil {
		return nil, err
	}
	path := EnvFilePath(name, c)
	_ = os.Remove(path)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	var once sync.Once
	remove = func() { once.Do(func() { _ = os.Remove(path) }) }
	if _, err := f.WriteString(b.String()); err != nil {
		_ = f.Close()
		remove()
		return nil, err
	}
	if err := f.Close(); err != nil {
		remove()
		return nil, err
	}
	return remove, nil
}
