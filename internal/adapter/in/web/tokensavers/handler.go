// The Token savers page, /token-savers: rtk, caveman and codegraph with
// their install state, the installed and pinned versions, and Install,
// Update and Remove (ADR 0012). The server paints the list from
// ports.TokenSaverInstallReader (the same shape list_token_savers answers);
// the browser's tokenSaversPage takes it over and writes through /api.

package tokensavers

import (
	"log/slog"
	"net/http"
	"time"

	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Href is the Token savers page.
const Href = "/token-savers"

// Handler serves GET /token-savers.
type Handler struct {
	Savers ports.TokenSaverInstallReader
	Layout shell.Layout
	Render shell.Renderer
	// Indexes lists the repository indexes; nil leaves their section out.
	Indexes ports.RepositoryIndexes
	// Now is the clock an index's age is painted with; nil is time.Now.
	Now func() time.Time
}

// Page renders the page. A failed read still renders it: the browser reads
// the list itself when there is no seed.
func (h Handler) Page(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	var v View
	list, err := h.Savers.ListTokenSavers(ctx)
	if err != nil {
		slog.Warn("token savers page: list not read; the browser reads it", "err", err)
	} else {
		v.Seed = list
	}
	v.Indexes = h.readIndexes(r)
	frame, err := h.Layout(ctx, "Token savers", "", "")
	if err != nil {
		return err
	}
	frame.Top.OnTokenSavers = true
	return h.Render(w, r, http.StatusOK, Page(frame, v))
}

// View is what the page renders.
type View struct {
	// Seed is nil when it could not be read.
	Seed *domain.TokenSaverList
	// Indexes is nil when repository indexes are not served.
	Indexes *IndexesView
}

// Unsupported is why nothing installs here, or "".
func (v View) Unsupported() string {
	if v.Seed == nil {
		return ""
	}
	return v.Seed.Unsupported
}

// Intensity is the caveman intensity the seed holds; full when it could
// not be read.
func (v View) Intensity() domain.CavemanIntensity {
	if v.Seed == nil || v.Seed.CavemanIntensity == "" {
		return domain.CavemanFull
	}
	return v.Seed.CavemanIntensity
}

// Row is one saver as the server paints it before the browser takes over;
// the browser's domain/saver.js has the same rules.
type Row struct {
	Saver, Summary, StateLabel, Tone, Versions string
	State                                      domain.TokenSaverState
	// NotInstalled is NotInstalledNotice for a saver on somewhere but not
	// installed, else "".
	NotInstalled string
}

// NotInstalledNotice flags a saver on somewhere but not installed.
const NotInstalledNotice = "Turned on for some sessions but not installed, so they run without it."

// Rows are the seed's savers in order.
func (v View) Rows() []Row {
	if v.Seed == nil {
		return nil
	}
	rows := make([]Row, 0, len(v.Seed.Savers))
	for _, s := range v.Seed.Savers {
		rows = append(rows, NewRow(s))
	}
	return rows
}

// NewRow paints s.
func NewRow(s domain.TokenSaverStatus) Row {
	r := Row{Saver: string(s.Saver), Summary: s.Summary, State: s.State}
	if s.OnSomewhere && s.Installed == nil {
		r.NotInstalled = NotInstalledNotice
	}
	switch s.State {
	case domain.TokenSaverInstalled:
		r.StateLabel, r.Tone = "Installed", "signal"
	case domain.TokenSaverOutdated:
		r.StateLabel, r.Tone = "Update available", "attention"
	case domain.TokenSaverInstalling:
		r.StateLabel = "Installing"
	case domain.TokenSaverFailed:
		r.StateLabel, r.Tone = "Install failed", "danger"
	default:
		r.StateLabel = "Not installed"
	}
	switch {
	case s.Installed == nil:
		r.Versions = "Pins " + s.PinnedVersion
	case s.Installed.Version == s.PinnedVersion:
		r.Versions = "Installed " + s.PinnedVersion
	default:
		r.Versions = s.Installed.Version + " installed · pins " + s.PinnedVersion
	}
	return r
}
