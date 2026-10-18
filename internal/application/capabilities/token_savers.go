package capabilities

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var (
	_ ports.TokenSaverInstalls      = (*Service)(nil)
	_ ports.TokenSaverInstallations = (*Service)(nil)
)

// TokenSaverDeps are what installing token savers needs (ADR 0012).
type TokenSaverDeps struct {
	Installations ports.TokenSaverInstallationRepository
	Downloads     ports.ReleaseDownloads
	// Dir is <data dir>/tools: each saver installs under <Dir>/<saver>/<version>.
	Dir string
	// ReleaseBaseURL replaces DefaultTokenSaverReleaseBase when set.
	ReleaseBaseURL string
	// Releases replaces PinnedTokenSaverReleases when set.
	Releases []TokenSaverRelease
	// Platform replaces the server's own (runtime.GOOS/GOARCH) when set.
	Platform domain.Platform
}

// tokenSavers installs savers; one install or remove per saver at a time.
type tokenSavers struct {
	TokenSaverDeps

	mu   sync.Mutex
	jobs map[domain.TokenSaver]*installJob
}

// installJob is a saver's install while it runs, then its failure until the
// next install or remove. A successful one is dropped.
type installJob struct {
	running  bool
	progress string
	err      *domain.TokenSaverError
}

// UseTokenSavers enables installing token savers. Until it is called their
// use cases answer UNAVAILABLE.
func (s *Service) UseTokenSavers(d TokenSaverDeps) {
	if d.ReleaseBaseURL == "" {
		d.ReleaseBaseURL = DefaultTokenSaverReleaseBase
	}
	d.ReleaseBaseURL = strings.TrimRight(d.ReleaseBaseURL, "/")
	if d.Releases == nil {
		d.Releases = PinnedTokenSaverReleases()
	}
	if d.Platform == (domain.Platform{}) {
		d.Platform = domain.Platform{OS: runtime.GOOS, Arch: runtime.GOARCH}
	}
	s.savers = &tokenSavers{TokenSaverDeps: d, jobs: map[domain.TokenSaver]*installJob{}}
}

func (s *Service) tokenSavers() (*tokenSavers, error) {
	if s.savers == nil {
		return nil, domain.TokenSaverErr("UNAVAILABLE", "token savers are not configured on this server")
	}
	return s.savers, nil
}

// ListTokenSavers answers every saver with its install state.
func (s *Service) ListTokenSavers(context.Context) (*domain.TokenSaverList, error) {
	t, err := s.tokenSavers()
	if err != nil {
		return nil, err
	}
	installed, err := t.installed()
	if err != nil {
		return nil, err
	}
	list := &domain.TokenSaverList{Platform: t.Platform.String(), Supported: t.Platform.Supported()}
	if !list.Supported {
		list.Unsupported = domain.UnsupportedPlatformMessage(t.Platform)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, rel := range t.Releases {
		list.Savers = append(list.Savers, t.status(rel, installed[rel.Saver]))
	}
	return list, nil
}

// InstalledTokenSavers answers the installed savers, for preparing a session.
func (s *Service) InstalledTokenSavers(context.Context) ([]*domain.TokenSaverInstallation, error) {
	t, err := s.tokenSavers()
	if err != nil {
		return nil, err
	}
	return t.Installations.List()
}

// InstallTokenSaver starts installing the pinned version of saver.
func (s *Service) InstallTokenSaver(_ context.Context, saver domain.TokenSaver) (*domain.TokenSaverStatus, error) {
	t, err := s.tokenSavers()
	if err != nil {
		return nil, err
	}
	rel, err := t.release(saver)
	if err != nil {
		return nil, err
	}
	asset, ok := rel.Assets[t.Platform]
	if !t.Platform.Supported() || !ok {
		return nil, domain.TokenSaverErr("TOKEN_SAVER_UNSUPPORTED_PLATFORM", domain.UnsupportedPlatformMessage(t.Platform))
	}
	installed, err := t.installed()
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if job := t.jobs[saver]; job == nil || !job.running {
		job = &installJob{running: true, progress: "Starting"}
		t.jobs[saver] = job
		go t.install(rel, asset, job, s.publish)
	}
	st := t.status(rel, installed[saver])
	return &st, nil
}

// RemoveTokenSaver deletes saver's files, every version, and its installation.
func (s *Service) RemoveTokenSaver(_ context.Context, saver domain.TokenSaver) (*domain.TokenSaverStatus, error) {
	t, err := s.tokenSavers()
	if err != nil {
		return nil, err
	}
	rel, err := t.release(saver)
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if job := t.jobs[saver]; job != nil && job.running {
		return nil, domain.TokenSaverErr("TOKEN_SAVER_BUSY", string(saver)+" is installing; remove it once the install ends")
	}
	delete(t.jobs, saver)
	if err := os.RemoveAll(filepath.Join(t.Dir, string(saver))); err != nil {
		return nil, err
	}
	if err := t.Installations.Delete(saver); err != nil {
		return nil, err
	}
	st := t.status(rel, nil)
	return &st, nil
}

func (t *tokenSavers) release(saver domain.TokenSaver) (TokenSaverRelease, error) {
	for _, rel := range t.Releases {
		if rel.Saver == saver {
			return rel, nil
		}
	}
	return TokenSaverRelease{}, domain.TokenSaverErr("TOKEN_SAVER_NOT_FOUND", fmt.Sprintf("there is no token saver %q; the harness has rtk, caveman and codegraph", saver))
}

func (t *tokenSavers) installed() (map[domain.TokenSaver]*domain.TokenSaverInstallation, error) {
	list, err := t.Installations.List()
	if err != nil {
		return nil, err
	}
	out := make(map[domain.TokenSaver]*domain.TokenSaverInstallation, len(list))
	for _, in := range list {
		out[in.Saver] = in
	}
	return out, nil
}

// status is rel's saver as the page shows it; t.mu is held.
func (t *tokenSavers) status(rel TokenSaverRelease, in *domain.TokenSaverInstallation) domain.TokenSaverStatus {
	st := domain.TokenSaverStatus{Saver: rel.Saver, Summary: rel.Summary, PinnedVersion: rel.Version, Installed: in}
	switch {
	case in == nil:
		st.State = domain.TokenSaverNotInstalled
	case in.Version == rel.Version:
		st.State = domain.TokenSaverInstalled
	default:
		st.State = domain.TokenSaverOutdated
	}
	if job := t.jobs[rel.Saver]; job != nil {
		if job.running {
			st.State, st.Progress = domain.TokenSaverInstalling, job.progress
		} else if job.err != nil {
			st.State, st.Error = domain.TokenSaverFailed, job.err
		}
	}
	return st
}

func (t *tokenSavers) report(job *installJob, progress string) {
	t.mu.Lock()
	job.progress = progress
	t.mu.Unlock()
}

// install runs in the background: download into a staging directory under
// Dir, verify, unpack, and only then move the version into place and record
// it. Whatever fails, the staging directory goes and the previous
// installation stays as it was.
func (t *tokenSavers) install(rel TokenSaverRelease, asset TokenSaverAsset, job *installJob, publish func(domain.Event) error) {
	in, err := t.fetch(context.Background(), rel, asset, job)
	t.mu.Lock()
	job.running = false
	if err != nil {
		var se *domain.StructuredError
		if !errors.As(err, &se) {
			se = domain.TokenSaverErr("TOKEN_SAVER_INSTALL_FAILED", fmt.Sprintf("Could not install %s %s: %v", rel.Saver, rel.Version, err))
		}
		job.err = &domain.TokenSaverError{Code: se.Code, Message: se.Message}
		t.mu.Unlock()
		slog.Warn("token saver install failed", "saver", rel.Saver, "version", rel.Version, "err", err)
		return
	}
	delete(t.jobs, rel.Saver)
	t.mu.Unlock()
	slog.Info("token saver installed", "saver", rel.Saver, "version", rel.Version, "path", in.Path)
	if err := publish(domain.TokenSaverInstallCompleted{Saver: rel.Saver, Version: rel.Version}); err != nil {
		slog.Warn("token saver installed: a subscriber failed", "saver", rel.Saver, "err", err)
	}
}

func (t *tokenSavers) fetch(ctx context.Context, rel TokenSaverRelease, asset TokenSaverAsset, job *installJob) (*domain.TokenSaverInstallation, error) {
	if err := os.MkdirAll(t.Dir, 0o755); err != nil {
		return nil, err
	}
	staging, err := os.MkdirTemp(t.Dir, ".install-"+string(rel.Saver)+"-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(staging)

	want := rel.SHA256
	if want == "" {
		t.report(job, "Downloading "+rel.Checksums)
		if want, err = t.checksum(ctx, rel, asset); err != nil {
			return nil, err
		}
	}
	got, err := t.download(ctx, rel, asset, filepath.Join(staging, asset.Name), job)
	if err != nil {
		return nil, err
	}
	t.report(job, "Verifying "+asset.Name)
	if !strings.EqualFold(got, want) {
		against := "its checksum in " + rel.Checksums
		if rel.SHA256 != "" {
			against = "the sha256 the harness pins"
		}
		return nil, domain.TokenSaverErr("TOKEN_SAVER_CHECKSUM_MISMATCH", fmt.Sprintf(
			"%s does not match %s (got %s, expected %s). Nothing was installed.", asset.Name, against, got, want))
	}

	t.report(job, "Unpacking "+asset.Name)
	unpacked := filepath.Join(staging, "unpacked")
	entry, err := unpack(filepath.Join(staging, asset.Name), unpacked, asset.Entry, rel.Only)
	if err != nil {
		return nil, domain.TokenSaverErr("TOKEN_SAVER_ARCHIVE_INVALID", fmt.Sprintf("%s could not be unpacked: %v. Nothing was installed.", asset.Name, err))
	}

	saverDir := filepath.Join(t.Dir, string(rel.Saver))
	final := filepath.Join(saverDir, rel.Version)
	if err := os.MkdirAll(saverDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.RemoveAll(final); err != nil {
		return nil, err
	}
	if err := os.Rename(unpacked, final); err != nil {
		return nil, err
	}
	in := &domain.TokenSaverInstallation{
		Saver: rel.Saver, Version: rel.Version, Dir: final,
		Path: filepath.Join(final, filepath.FromSlash(entry)), InstalledAt: time.Now().UTC(),
	}
	if err := t.Installations.Put(in); err != nil {
		_ = os.RemoveAll(final)
		return nil, err
	}
	// The pinned version is in place: every other one goes.
	others, _ := os.ReadDir(saverDir)
	for _, o := range others {
		if o.Name() != rel.Version {
			_ = os.RemoveAll(filepath.Join(saverDir, o.Name()))
		}
	}
	return in, nil
}

func (t *tokenSavers) url(rel TokenSaverRelease, file string) string {
	return t.ReleaseBaseURL + "/" + rel.Repo + "/releases/download/" + rel.Version + "/" + file
}

// checksum reads asset's digest from the release's checksum file.
func (t *tokenSavers) checksum(ctx context.Context, rel TokenSaverRelease, asset TokenSaverAsset) (string, error) {
	var buf bytes.Buffer
	url := t.url(rel, rel.Checksums)
	if err := t.Downloads.Download(ctx, url, &limitedWriter{w: &buf, left: 1 << 20}, nil); err != nil {
		if errors.Is(err, ports.ErrReleaseFileNotFound) {
			return "", domain.TokenSaverErr("TOKEN_SAVER_CHECKSUM_MISSING", fmt.Sprintf(
				"%s %s publishes no %s to verify %s against (HTTP 404 from %s).", rel.Saver, rel.Version, rel.Checksums, asset.Name, url))
		}
		return "", downloadFailed(rel.Checksums, err)
	}
	sc := bufio.NewScanner(&buf)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == asset.Name {
			return fields[0], nil
		}
	}
	return "", domain.TokenSaverErr("TOKEN_SAVER_CHECKSUM_MISSING", fmt.Sprintf(
		"%s in %s %s has no line for %s. Nothing was installed.", rel.Checksums, rel.Saver, rel.Version, asset.Name))
}

// download saves asset to path and answers its sha256.
func (t *tokenSavers) download(ctx context.Context, rel TokenSaverRelease, asset TokenSaverAsset, path string, job *installJob) (string, error) {
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	url := t.url(rel, asset.Name)
	err = t.Downloads.Download(ctx, url, &limitedWriter{w: io.MultiWriter(f, h), left: maxAssetBytes}, func(done, total int64) {
		t.report(job, "Downloading "+asset.Name+" — "+byteProgress(done, total))
	})
	if errors.Is(err, ports.ErrReleaseFileNotFound) {
		return "", domain.TokenSaverErr("TOKEN_SAVER_ASSET_MISSING", fmt.Sprintf(
			"%s %s has no asset %s (HTTP 404 from %s).", rel.Saver, rel.Version, asset.Name, url))
	}
	if err != nil {
		return "", downloadFailed(asset.Name, err)
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func downloadFailed(file string, err error) error {
	return domain.TokenSaverErr("TOKEN_SAVER_DOWNLOAD_FAILED", fmt.Sprintf("Could not download %s: %v. Nothing was installed.", file, err))
}

// maxAssetBytes bounds a download: codegraph's bundle, the largest, is about 63 MB.
const maxAssetBytes = 1 << 30

// limitedWriter refuses to write past its limit.
type limitedWriter struct {
	w    io.Writer
	left int64
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > l.left {
		return 0, errors.New("the file is larger than expected")
	}
	l.left -= int64(len(p))
	return l.w.Write(p)
}

// byteProgress is "1.2 of 58.3 MB", or "1.2 MB" with no total.
func byteProgress(done, total int64) string {
	if total <= 0 {
		return byteSize(done)
	}
	return byteSize(done) + " of " + byteSize(total)
}

func byteSize(n int64) string {
	switch {
	case n < 1<<10:
		return fmt.Sprintf("%d B", n)
	case n < 1<<20:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	}
}
