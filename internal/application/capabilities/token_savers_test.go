package capabilities

import (
	"context"
	"io"
	"testing"
	"time"

	"operators-mcp/internal/domain"
)

// Closing the token savers, as the server stops, cancels an install's
// download and waits for the install to end; none starts afterwards.
func TestClosingTokenSaversCancelsAndWaitsForTheirInstalls(t *testing.T) {
	downloads := &blockingDownloads{started: make(chan struct{}, 1)}
	installs := &memoryInstallations{}
	s := NewService(Deps{})
	s.UseTokenSavers(TokenSaverDeps{
		Installations: installs, Downloads: downloads, Dir: t.TempDir(),
		Platform: domain.Platform{OS: "darwin", Arch: "arm64"},
	})
	if _, err := s.InstallTokenSaver(context.Background(), domain.TokenSaverRTK); err != nil {
		t.Fatal(err)
	}
	select {
	case <-downloads.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the install never started downloading")
	}

	closed := make(chan struct{})
	go func() {
		s.CloseTokenSavers()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("closing did not cancel the running download")
	}
	if !downloads.cancelled {
		t.Error("the download returned without its context cancelled")
	}
	if len(installs.list) != 0 {
		t.Errorf("a cancelled install recorded %+v", installs.list)
	}
	if _, err := s.InstallTokenSaver(context.Background(), domain.TokenSaverRTK); err == nil {
		t.Error("an install started after closing")
	}
}

// blockingDownloads holds every download until its context ends.
type blockingDownloads struct {
	started   chan struct{}
	cancelled bool
}

func (d *blockingDownloads) Download(ctx context.Context, _ string, _ io.Writer, _ func(done, total int64)) error {
	d.started <- struct{}{}
	<-ctx.Done()
	d.cancelled = true
	return ctx.Err()
}

type memoryInstallations struct{ list []*domain.TokenSaverInstallation }

func (m *memoryInstallations) List() ([]*domain.TokenSaverInstallation, error) { return m.list, nil }

func (m *memoryInstallations) Put(in *domain.TokenSaverInstallation) error {
	m.list = append(m.list, in)
	return nil
}

func (m *memoryInstallations) Delete(domain.TokenSaver) error { return nil }
