package ports

import (
	"context"
	"errors"
	"io"

	"operators-mcp/internal/domain"
)

// Token savers, in the capabilities context: their installations, downloaded
// from the releases the harness pins (ADR 0012). *capabilities.Service
// satisfies the driving ports.

// TokenSaverInstalls installs, updates and removes token savers (driving):
// the Token savers page.
type TokenSaverInstalls interface {
	TokenSaverInstallReader
	// InstallTokenSaver installs the pinned version in the background,
	// replacing any other installed version (whose files stay until the
	// harness starts again: running sessions may use them), and answers the
	// saver's status
	// as the install starts; its progress and outcome are in later lists.
	// Refuses TOKEN_SAVER_NOT_FOUND, TOKEN_SAVER_UNSUPPORTED_PLATFORM and
	// TOKEN_SAVER_BUSY.
	InstallTokenSaver(ctx context.Context, saver domain.TokenSaver) (*domain.TokenSaverStatus, error)
	// RemoveTokenSaver deletes the saver's files and its installation.
	// Refuses TOKEN_SAVER_NOT_FOUND and TOKEN_SAVER_BUSY.
	RemoveTokenSaver(ctx context.Context, saver domain.TokenSaver) (*domain.TokenSaverStatus, error)
}

// TokenSaverInstallReader lists every token saver with its install state.
type TokenSaverInstallReader interface {
	ListTokenSavers(ctx context.Context) (*domain.TokenSaverList, error)
}

// TokenSaverInstallations is what preparing a session reads: the savers
// installed on this machine and the file each is used through.
type TokenSaverInstallations interface {
	InstalledTokenSavers(ctx context.Context) ([]*domain.TokenSaverInstallation, error)
}

// TokenSaverInstallationRepository stores installations, one per saver
// (driven).
type TokenSaverInstallationRepository interface {
	List() ([]*domain.TokenSaverInstallation, error)
	// Put creates or replaces the saver's installation.
	Put(in *domain.TokenSaverInstallation) error
	// Delete removes it; a missing one is not an error.
	Delete(saver domain.TokenSaver) error
}

// ReleaseDownloads fetches release files over HTTP (driven).
type ReleaseDownloads interface {
	// Download writes the file at url to w, calling progress (when not nil)
	// as bytes arrive with the total, or -1 when the server does not say.
	// A file the server does not have is ErrReleaseFileNotFound.
	Download(ctx context.Context, url string, w io.Writer, progress func(done, total int64)) error
}

// ErrReleaseFileNotFound is a release file the server answers 404 for.
var ErrReleaseFileNotFound = errors.New("release file not found")
