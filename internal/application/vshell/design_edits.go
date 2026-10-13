package vshell

import (
	"bytes"
	"context"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Designs written through the edit proxy: a file created, changed or
// removed in task/designs publishes, revises or unpublishes an artifact,
// whose bytes the harness keeps; mv to project/designs and back changes
// its scope. A design keeps its path relative to the designs folder.

const taskDesignsDir = "task/designs/"

func refuseDesignNotOnTask(rel string) ports.ShellAnswer {
	return refuse("NOT_ON_TASK", rel+" cannot move to task scope: the design was not published on this task")
}

func refuseNoDesigns(rel string) ports.ShellAnswer {
	return refuse("NOT_STORED", rel+": this server stores no designs from the shell; nothing was written")
}

// checkDesignsAt: the designs at rel, or in the folder rel, may be written
// or removed while the session's copies are the stored revisions; a new
// file becomes a design.
func (s *Service) checkDesignsAt(sh *shell, rel string) ports.ShellAnswer {
	if s.src.Designs == nil {
		return refuseNoDesigns(rel)
	}
	for _, row := range sh.under(rel, ports.ShellArtifact) {
		if ans := s.stale(sh, row); !ans.Allow {
			return ans
		}
	}
	sh.hold(rel)
	return allow
}

// checkDesignMove: between task/designs and project/designs a design
// changes scope, to task scope only when published on this task; within a
// folder it is only renamed. A file new to the harness may only land in
// task/designs.
func (s *Service) checkDesignMove(sh *shell, src, dst string) ports.ShellAnswer {
	if s.src.Designs == nil {
		return refuseNoDesigns(src)
	}
	rows := sh.under(src, ports.ShellArtifact)
	if len(rows) == 0 && classify(dst) != taskDesigns {
		return refuseUnstored(dst, classify(dst))
	}
	for _, row := range rows {
		if ans := s.stale(sh, row); !ans.Allow {
			return ans
		}
		if classify(moved(row.Path, src, dst)) == taskDesigns && row.Scope == string(domain.ArtifactScopeProject) {
			if a := s.design(row.ID); a == nil || a.TicketID != sh.ticketID {
				return refuseDesignNotOnTask(row.Path)
			}
		}
		if over, ok := sh.files[moved(row.Path, src, dst)]; ok && over.ID != row.ID {
			if ans := s.stale(sh, over); !ans.Allow {
				return ans
			}
		}
	}
	sh.hold(src)
	sh.hold(dst)
	return allow
}

// commitDesignWrite stores the file at rel: a new revision of its design,
// or a new design published on the task.
func (s *Service) commitDesignWrite(sh *shell, rel string) ports.ShellAnswer {
	if s.src.Designs == nil {
		return refuseNoDesigns(rel)
	}
	info, err := os.Stat(sh.abs(rel))
	if err != nil || info.IsDir() {
		return allow // nothing was written
	}
	content, err := os.ReadFile(sh.abs(rel))
	if err != nil {
		return allow
	}
	ctx := context.Background()
	row, ok := sh.files[rel]
	var a *domain.Artifact
	switch {
	case !ok:
		a, err = s.src.Designs.PublishDesign(ctx, sh.sessionID, strings.TrimPrefix(rel, taskDesignsDir), designTitle(rel, content), content)
	case row.Kind != ports.ShellArtifact:
		return allow
	default:
		if now := s.storedVersion(sh, row); now != row.Version {
			s.restorePath(sh, rel)
			return refuseStale(rel, now)
		}
		if s.storedBytesAre(row.ID, content) {
			return allow
		}
		a, err = s.src.Designs.ReviseDesign(ctx, row.ID, content)
	}
	if err != nil {
		ans := refuseErr(err)
		s.restorePath(sh, rel)
		return ans
	}
	sh.files[rel] = designRow(rel, a)
	return sh.saveIndex()
}

// commitDesignRemove unpublishes the designs whose files under rel are
// gone.
func (s *Service) commitDesignRemove(sh *shell, rel string) ports.ShellAnswer {
	if s.src.Designs == nil {
		return refuseNoDesigns(rel)
	}
	for _, row := range sh.under(rel, ports.ShellArtifact) {
		if _, err := os.Lstat(sh.abs(row.Path)); err == nil {
			continue // the removal did not happen
		}
		if err := s.src.Designs.RemoveDesign(context.Background(), row.ID); err != nil && !errs.HasCode(err, "ARTIFACT_NOT_FOUND") {
			ans := refuseErr(err)
			s.restorePath(sh, row.Path)
			return ans
		}
		delete(sh.files, row.Path)
	}
	return sh.saveIndex()
}

// commitDesignMove moves the designs under src to dst, once they are
// there: scope follows the folder; a design the move wrote over is
// unpublished. A file new to the harness moved into task/designs is
// published.
func (s *Service) commitDesignMove(sh *shell, src, dst string) ports.ShellAnswer {
	if s.src.Designs == nil {
		return refuseNoDesigns(src)
	}
	rows := sh.under(src, ports.ShellArtifact)
	if len(rows) == 0 {
		if classify(dst) != taskDesigns {
			return allow
		}
		return s.commitDesignWrite(sh, dst)
	}
	ctx := context.Background()
	for _, row := range rows {
		to := moved(row.Path, src, dst)
		if _, err := os.Lstat(sh.abs(to)); err != nil {
			continue // the move did not happen
		}
		if over, ok := sh.files[to]; ok && over.ID != row.ID {
			if err := s.src.Designs.RemoveDesign(ctx, over.ID); err != nil && !errs.HasCode(err, "ARTIFACT_NOT_FOUND") {
				return s.undoMove(sh, row.Path, to, refuseErr(err))
			}
			delete(sh.files, to)
		}
		a := s.design(row.ID)
		if a == nil || strconv.Itoa(a.Revision) != row.Version {
			return s.undoMove(sh, row.Path, to, s.stale(sh, row))
		}
		scope := domain.ArtifactScopeProject
		if classify(to) == taskDesigns {
			scope = domain.ArtifactScopeTask
		}
		if scope != a.Scope {
			if scope == domain.ArtifactScopeTask && a.TicketID != sh.ticketID {
				return s.undoMove(sh, row.Path, to, refuseDesignNotOnTask(row.Path))
			}
			var err error
			if a, err = s.src.Designs.SetArtifactScope(ctx, a.ID, scope); err != nil {
				return s.undoMove(sh, row.Path, to, refuseErr(err))
			}
		}
		delete(sh.files, row.Path)
		sh.files[to] = designRow(to, a)
	}
	return sh.saveIndex()
}

// undoMove puts a design the harness did not move back where it was, and
// answers ans.
func (s *Service) undoMove(sh *shell, from, to string, ans ports.ShellAnswer) ports.ShellAnswer {
	if _, err := os.Lstat(sh.abs(from)); err != nil {
		_ = os.MkdirAll(filepath.Dir(sh.abs(from)), 0o755)
		_ = os.Rename(sh.abs(to), sh.abs(from))
	}
	s.restorePath(sh, from)
	return ans
}

// design is the artifact id as stored, or nil.
func (s *Service) design(id string) *domain.Artifact {
	if s.src.Designs == nil {
		return nil
	}
	a, err := s.src.Designs.GetArtifact(context.Background(), id)
	if err != nil {
		return nil
	}
	return a
}

// storedBytesAre says the artifact's stored file holds content already.
func (s *Service) storedBytesAre(id string, content []byte) bool {
	f, err := s.src.Designs.OpenArtifactFile(context.Background(), id, "")
	if err != nil {
		return false
	}
	defer f.Content.Close()
	if f.Size != int64(len(content)) {
		return false
	}
	stored := make([]byte, len(content))
	if _, err := io.ReadFull(f.Content, stored); err != nil {
		return false
	}
	return bytes.Equal(stored, content)
}

// designTitle is a new design's title: a page's first heading, else the
// file's name without its extension.
func designTitle(rel string, content []byte) string {
	if domain.InferArtifactKind(domain.ArtifactMIME(rel, content)) == domain.ArtifactPage {
		return TitleOf(rel, string(content))
	}
	name := path.Base(rel)
	if t := strings.TrimSuffix(name, path.Ext(name)); t != "" {
		return t
	}
	return name
}

func designRow(rel string, a *domain.Artifact) ports.ShellFile {
	return ports.ShellFile{Path: rel, Kind: ports.ShellArtifact, ID: a.ID, Scope: string(a.Scope),
		Version: strconv.Itoa(a.Revision), Title: a.Title, UpdatedAt: a.UpdatedAt}
}
