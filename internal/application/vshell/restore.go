package vshell

import (
	"log/slog"
	"os"
	"strings"
	"time"
)

// restore puts the paths of ed back as the harness holds them, after their
// commit was refused: the change is already on disk and was not stored, so
// left there it would read as if it had been. A file the shell was given
// is written again with its stored content and base version; a file it was
// not given is removed. sh is held.
func (s *Service) restore(sh *shell, ed edit) {
	for i, rel := range ed.rel {
		if !ed.in[i] {
			continue
		}
		if rel == indexFile {
			if err := sh.writeIndex(); err != nil {
				slog.Warn("restore a virtual shell's index", "session", sh.sessionID, "err", err)
			}
			continue
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
		if !known {
			if fi, err := os.Lstat(sh.abs(rel)); err == nil && fi.Mode().IsRegular() {
				_ = os.Remove(sh.abs(rel))
			}
		}
	}
	s.refreshLocked(sh)
}
