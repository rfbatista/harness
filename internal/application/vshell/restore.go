package vshell

import (
	"log/slog"
	"os"
	"strings"
	"time"
)

// restore puts the paths of ed back as the harness holds them, after their
// commit was refused: the change is already on disk and was not stored, so
// left there it would read as if it had been. sh is held.
func (s *Service) restore(sh *shell, ed edit) {
	for i, rel := range ed.rel {
		if ed.in[i] {
			sh.unstore(rel)
		}
	}
	s.refreshLocked(sh)
}

// restorePath is restore for the one path rel, released from its hold
// first. sh is held.
func (s *Service) restorePath(sh *shell, rel string) {
	sh.release(rel)
	sh.unstore(rel)
	s.refreshLocked(sh)
}

// unstore readies rel to be put back by the next refresh: a file the shell
// was given, and the files under rel, lose their base version so the
// refresh writes them again with their stored content; a file it was not
// given is removed; the index is rewritten. sh is held.
func (sh *shell) unstore(rel string) {
	if rel == indexFile {
		if err := sh.writeIndex(); err != nil {
			slog.Warn("restore a virtual shell's index", "session", sh.sessionID, "err", err)
		}
		return
	}
	known := false
	for p, row := range sh.files {
		if p == rel || strings.HasPrefix(p, rel+"/") {
			// Not current any more, so the refresh writes it again.
			row.Version, row.UpdatedAt = "", time.Time{}
			sh.files[p] = row
			known = known || p == rel
		}
	}
	if known {
		return
	}
	if fi, err := os.Lstat(sh.abs(rel)); err == nil && !fi.IsDir() {
		_ = os.Remove(sh.abs(rel))
	}
}
