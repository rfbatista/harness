// Package managedclaude installs the official native claude build into the
// harness data dir and switches between versions (ADR 0019).
//
// Layout under the data dir:
//
//	versions/<version>/claude   one directory per installed version
//	current                     symlink to versions/<version>/claude
//
// The filesystem is the record: there is no table, so a moved or copied data
// dir keeps working.
package managedclaude

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// DefaultBaseURL is the official release server, as claude.ai/install.sh
// names it.
const DefaultBaseURL = "https://downloads.claude.ai/claude-code-releases"

var _ ports.ManagedClaude = (*Service)(nil)

var versionRE = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9._-]+)?$`)

// Deps are what the service is built from.
type Deps struct {
	Downloads ports.ReleaseDownloads
	// Dir is the managed claude's directory (<data dir>/claude).
	Dir string
	// BaseURL is the release server; DefaultBaseURL when empty.
	BaseURL string
	// Platform is the release platform key ("darwin-arm64", "linux-x64",
	// "linux-x64-musl"); this machine's when empty.
	Platform string
	// RunningSessions counts the agent sessions running now; a switch names
	// that many as keeping the version they started with. Nil: none.
	RunningSessions func() int
}

// Service installs and switches managed claude versions.
type Service struct {
	downloads ports.ReleaseDownloads
	dir       string
	base      string
	platform  string
	running   func() int

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu         sync.Mutex // guards the fields below and the files under dir
	installing *ports.ManagedClaudeInstall
	failed     *ports.ManagedClaudeFailure
	// stopInstall ends the install that runs; installDone closes when it has
	// ended; cancelled says the person asked, so nothing is reported failed.
	stopInstall context.CancelFunc
	installDone chan struct{}
	cancelled   bool
}

// New builds the service and clears what an interrupted install left.
func New(d Deps) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{downloads: d.Downloads, dir: d.Dir, base: strings.TrimRight(d.BaseURL, "/"), platform: d.Platform, running: d.RunningSessions, ctx: ctx, cancel: cancel}
	if s.base == "" {
		s.base = DefaultBaseURL
	}
	if s.platform == "" {
		s.platform = HostPlatform()
	}
	if abs, err := filepath.Abs(s.dir); err == nil {
		s.dir = abs
	}
	if tmps, _ := filepath.Glob(filepath.Join(s.dir, "versions", ".tmp-*")); len(tmps) > 0 {
		for _, t := range tmps {
			os.RemoveAll(t)
		}
	}
	return s
}

// HostPlatform is this machine's key in a release manifest.
func HostPlatform() string {
	arch := "x64"
	if runtime.GOARCH == "arm64" {
		arch = "arm64"
	}
	if runtime.GOOS == "linux" {
		for _, p := range []string{"/lib/libc.musl-x86_64.so.1", "/lib/libc.musl-aarch64.so.1"} {
			if _, err := os.Stat(p); err == nil {
				return "linux-" + arch + "-musl"
			}
		}
		return "linux-" + arch
	}
	return runtime.GOOS + "-" + arch
}

// Close cancels the install that runs and waits for it.
func (s *Service) Close() {
	s.cancel()
	s.wg.Wait()
}

// Path is the stable path agents run.
func (s *Service) Path() string { return filepath.Join(s.dir, "current") }

func (s *Service) versionDir(v string) string { return filepath.Join(s.dir, "versions", v) }

// Installed says whether the current path runs an installed version.
func (s *Service) Installed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current() != ""
}

// current is the active version, "" when there is none or its file is gone.
func (s *Service) current() string {
	target, err := os.Readlink(s.Path())
	if err != nil {
		return ""
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(s.dir, target)
	}
	if _, err := os.Stat(target); err != nil {
		return ""
	}
	return filepath.Base(filepath.Dir(target))
}

// State lists what is installed and what is happening.
func (s *Service) State() ports.ManagedClaudeState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state()
}

func (s *Service) state() ports.ManagedClaudeState {
	cur := s.current()
	st := ports.ManagedClaudeState{Path: s.Path(), Current: cur, Installed: []ports.ManagedClaudeVersion{}, Failed: s.failed}
	if s.installing != nil {
		in := *s.installing
		st.Installing = &in
	}
	entries, _ := os.ReadDir(filepath.Join(s.dir, "versions"))
	for _, e := range entries {
		if !e.IsDir() || !versionRE.MatchString(e.Name()) {
			continue
		}
		if _, err := os.Stat(filepath.Join(s.versionDir(e.Name()), "claude")); err != nil {
			continue
		}
		v := ports.ManagedClaudeVersion{Version: e.Name(), Active: e.Name() == cur}
		if info, err := e.Info(); err == nil {
			v.InstalledAt = info.ModTime()
		}
		st.Installed = append(st.Installed, v)
	}
	sort.Slice(st.Installed, func(i, j int) bool { return st.Installed[i].InstalledAt.After(st.Installed[j].InstalledAt) })
	return st
}

// Releases names the newest versions the server has.
func (s *Service) Releases(ctx context.Context) (ports.ManagedClaudeReleases, error) {
	latest, err := s.channel(ctx, "latest")
	if err != nil {
		return ports.ManagedClaudeReleases{}, err
	}
	stable, err := s.channel(ctx, "stable")
	if err != nil {
		stable = latest
	}
	return ports.ManagedClaudeReleases{Latest: latest, Stable: stable}, nil
}

// channel resolves "latest" or "stable" to a version.
func (s *Service) channel(ctx context.Context, name string) (string, error) {
	body, err := s.fetch(ctx, s.base+"/"+name)
	if err != nil {
		return "", err
	}
	v := strings.TrimSpace(string(body))
	if !versionRE.MatchString(v) {
		return "", errors.New("the release server named no version for " + name)
	}
	return v, nil
}

func (s *Service) fetch(ctx context.Context, url string) ([]byte, error) {
	var buf bytes.Buffer
	if err := s.downloads.Download(ctx, url, &buf, nil); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func invalid(msg string) error { return &domain.StructuredError{Code: "INVALID_INPUT", Message: msg} }

// Install starts downloading a version.
func (s *Service) Install(_ context.Context, version string) (ports.ManagedClaudeState, error) {
	version = strings.TrimSpace(version)
	if version != "latest" && version != "stable" && !versionRE.MatchString(version) {
		return ports.ManagedClaudeState{}, invalid(`Enter a version such as 2.1.52, or "latest" or "stable".`)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return ports.ManagedClaudeState{}, &domain.StructuredError{Code: "UNAVAILABLE", Message: "the server is stopping"}
	}
	if s.installing != nil {
		return ports.ManagedClaudeState{}, &domain.StructuredError{Code: "MANAGED_CLAUDE_BUSY", Message: "Another install is running. Wait for it to finish."}
	}
	if versionRE.MatchString(version) {
		if _, err := os.Stat(filepath.Join(s.versionDir(version), "claude")); err == nil {
			return ports.ManagedClaudeState{}, &domain.StructuredError{Code: "MANAGED_CLAUDE_VERSION_INSTALLED", Message: version + " is already installed."}
		}
	}
	s.failed = nil
	s.installing = &ports.ManagedClaudeInstall{Version: version, Total: -1}
	ictx, stop := context.WithCancel(s.ctx)
	s.stopInstall, s.installDone, s.cancelled = stop, make(chan struct{}), false
	s.wg.Add(1)
	go s.run(ictx, version, s.installDone)
	return s.state(), nil
}

// Cancel stops the install that runs and waits for it to end: what it had
// downloaded is removed and no failure is reported. With none running it
// changes nothing.
func (s *Service) Cancel() (ports.ManagedClaudeState, error) {
	s.mu.Lock()
	if s.installing == nil {
		defer s.mu.Unlock()
		return s.state(), nil
	}
	s.cancelled = true
	s.stopInstall()
	done := s.installDone
	s.mu.Unlock()
	<-done
	return s.State(), nil
}

// Remove deletes an installed version that is not the current one.
func (s *Service) Remove(version string) (ports.ManagedClaudeState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !versionRE.MatchString(version) {
		return ports.ManagedClaudeState{}, invalid("Enter an installed version.")
	}
	if _, err := os.Stat(filepath.Join(s.versionDir(version), "claude")); err != nil {
		return ports.ManagedClaudeState{}, &domain.StructuredError{Code: "MANAGED_CLAUDE_VERSION_NOT_FOUND", Message: version + " is not installed."}
	}
	if version == s.current() {
		return ports.ManagedClaudeState{}, &domain.StructuredError{Code: "MANAGED_CLAUDE_VERSION_CURRENT", Message: version + " is the version agents use. Switch to another version first."}
	}
	if err := os.RemoveAll(s.versionDir(version)); err != nil {
		return ports.ManagedClaudeState{}, err
	}
	return s.state(), nil
}

func (s *Service) progress(done, total int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.installing != nil {
		s.installing.Done, s.installing.Total = done, total
	}
}

func (s *Service) run(ctx context.Context, requested string, done chan struct{}) {
	defer s.wg.Done()
	defer close(done)
	version, err := s.install(ctx, requested)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.installing = nil
	if s.cancelled && err != nil {
		return
	}
	if err != nil {
		if version == "" {
			version = requested
		}
		s.failed = &ports.ManagedClaudeFailure{Version: version, Message: err.Error()}
		return
	}
	// The first install on an empty machine has nothing to choose from.
	if s.current() == "" {
		if err := s.activate(version); err != nil {
			s.failed = &ports.ManagedClaudeFailure{Version: version, Message: err.Error()}
		}
	}
}

type manifest struct {
	Platforms map[string]struct {
		Checksum string `json:"checksum"`
		Size     int64  `json:"size"`
	} `json:"platforms"`
}

// install downloads, verifies and places a version; nothing is placed unless
// the checksum matches. Answers the concrete version it worked on.
func (s *Service) install(ctx context.Context, requested string) (string, error) {
	version := requested
	if requested == "latest" || requested == "stable" {
		v, err := s.channel(ctx, requested)
		if err != nil {
			return "", fmt.Errorf("could not look up the %s version: %w", requested, err)
		}
		version = v
		s.mu.Lock()
		s.installing.Version = v
		s.mu.Unlock()
		if _, err := os.Stat(filepath.Join(s.versionDir(v), "claude")); err == nil {
			return v, nil // already there
		}
	}
	raw, err := s.fetch(ctx, s.base+"/"+version+"/manifest.json")
	if err != nil {
		if errors.Is(err, ports.ErrReleaseFileNotFound) {
			return version, fmt.Errorf("version %s does not exist", version)
		}
		return version, fmt.Errorf("could not read the release manifest: %w", err)
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return version, errors.New("the release manifest is not valid")
	}
	entry, ok := m.Platforms[s.platform]
	if !ok || len(entry.Checksum) != 64 {
		return version, fmt.Errorf("version %s has no build for %s", version, s.platform)
	}

	versions := filepath.Join(s.dir, "versions")
	if err := os.MkdirAll(versions, 0o755); err != nil {
		return version, err
	}
	tmp, err := os.MkdirTemp(versions, ".tmp-"+version+"-")
	if err != nil {
		return version, err
	}
	defer os.RemoveAll(tmp)
	f, err := os.OpenFile(filepath.Join(tmp, "claude"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return version, err
	}
	sum := sha256.New()
	err = s.downloads.Download(ctx, s.base+"/"+version+"/"+s.platform+"/claude", io.MultiWriter(f, sum), func(done, total int64) {
		if total < 0 {
			total = entry.Size
			if total <= 0 {
				total = -1
			}
		}
		s.progress(done, total)
	})
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return version, fmt.Errorf("download failed: %w", err)
	}
	if got := hex.EncodeToString(sum.Sum(nil)); got != entry.Checksum {
		return version, errors.New("checksum mismatch: the download is not the build claude published")
	}
	if err := os.Chmod(filepath.Join(tmp, "claude"), 0o755); err != nil {
		return version, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dst := s.versionDir(version)
	if err := os.Rename(tmp, dst); err != nil {
		return version, err
	}
	return version, nil
}

// Switch makes a version the one agents use and removes the other versions.
func (s *Service) Switch(version string) (ports.ManagedClaudeState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !versionRE.MatchString(version) {
		return ports.ManagedClaudeState{}, invalid("Enter an installed version.")
	}
	was := s.current()
	if err := s.activate(version); err != nil {
		return ports.ManagedClaudeState{}, err
	}
	st := s.state()
	if was != "" && was != version && s.running != nil {
		st.KeepingSessions = s.running()
	}
	return st, nil
}

// activate points current at the version, atomically, then removes the old
// versions. The caller holds mu.
func (s *Service) activate(version string) error {
	if _, err := os.Stat(filepath.Join(s.versionDir(version), "claude")); err != nil {
		return &domain.StructuredError{Code: "MANAGED_CLAUDE_VERSION_NOT_FOUND", Message: version + " is not installed."}
	}
	tmpLink := filepath.Join(s.dir, ".current-"+version)
	os.Remove(tmpLink)
	if err := os.Symlink(filepath.Join("versions", version, "claude"), tmpLink); err != nil {
		return err
	}
	if err := os.Rename(tmpLink, s.Path()); err != nil {
		os.Remove(tmpLink)
		return err
	}
	entries, _ := os.ReadDir(filepath.Join(s.dir, "versions"))
	for _, e := range entries {
		if e.IsDir() && e.Name() != version && !strings.HasPrefix(e.Name(), ".tmp-") {
			os.RemoveAll(s.versionDir(e.Name()))
		}
	}
	return nil
}

// Resolver says which claude binary agents run: the -claude.bin override when
// the person set one, else the managed current path.
type Resolver struct {
	// Override is Config.ClaudeBin; "" and "claude" (the flag's default) mean
	// none.
	Override string
	Managed  interface {
		Path() string
		Installed() bool
	}
}

var _ ports.ManagedClaudeBin = Resolver{}

func (r Resolver) Overridden() bool { return r.Override != "" && r.Override != "claude" }

func (r Resolver) Bin() string {
	if r.Overridden() {
		return r.Override
	}
	return r.Managed.Path()
}

func (r Resolver) Installed() bool { return r.Overridden() || r.Managed.Installed() }
