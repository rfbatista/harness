package vfs

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// What a session does to its task's instruction draft, task/instruction-drafts/
// in its shell: every file is writable, and a write is checked and stored as
// the task's draft, never as a document (ADR 0092).

const instructionDraftsDir = domain.InstructionDraftsDir

// inInstructionDrafts says rel is the draft folder or in it, and its path
// below it.
func inInstructionDrafts(rel string) (string, bool) {
	if rel == instructionDraftsDir {
		return "", true
	}
	return strings.CutPrefix(rel, instructionDraftsDir+"/")
}

// touchesInstructionDrafts says ed has a path inside the draft folder.
func touchesInstructionDrafts(ed edit) bool {
	for i, rel := range ed.rel {
		if _, ok := inInstructionDrafts(rel); ok && ed.in[i] {
			return true
		}
	}
	return false
}

func refuseNoDrafts(rel string) ports.ShellAnswer {
	return refuse("NOT_STORED", rel+": this server stores no instruction drafts from the shell; nothing was written")
}

// checkInstructionDrafts is the check of an edit that touches the draft.
func (s *Service) checkInstructionDrafts(ctx context.Context, sh *root, ed edit) ports.ShellAnswer {
	for i, rel := range ed.rel {
		if _, ok := inInstructionDrafts(rel); !ok || !ed.in[i] {
			continue
		}
		if sh.ticketID == "" {
			return refuseNoTask(rel)
		}
		if s.src.InstructionDrafts == nil {
			return refuseNoDrafts(rel)
		}
	}
	if ed.op == "mkdir" {
		return allow
	}
	if ed.op == "mv" && ed.in[0] && ed.in[1] {
		_, fromIn := inInstructionDrafts(ed.rel[0])
		_, toIn := inInstructionDrafts(ed.rel[1])
		if fromIn != toIn {
			return refuseCrossMove(ed.rel[0], ed.rel[1])
		}
	}
	for i, rel := range ed.rel {
		sub, ok := inInstructionDrafts(rel)
		if !ok || !ed.in[i] {
			continue
		}
		// The source of a move and what an rm removes are the files the
		// session holds; the destination of a move and a write are new content.
		removing := ed.op == "rm" || (ed.op == "mv" && i == 0)
		if removing {
			for _, row := range sh.under(rel, ports.ShellInstructionDraft) {
				rowSub, _ := inInstructionDrafts(row.Path)
				if err := s.src.InstructionDrafts.CheckInstructionDraft(ctx, sh.projID, sh.ticketID, rowSub); err != nil {
					return refuseErr(err)
				}
				if ans := s.stale(sh, row); !ans.Allow {
					return ans
				}
			}
		} else {
			// A move's destination may be a folder, whose files are what is
			// checked, each at its new path.
			targets := []string{sub}
			if ed.op == "mv" {
				targets = targets[:0]
				for _, row := range sh.under(ed.rel[0], ports.ShellInstructionDraft) {
					targets = append(targets, strings.TrimPrefix(moved(row.Path, ed.rel[0], rel), instructionDraftsDir+"/"))
				}
				if len(targets) == 0 {
					targets = append(targets, sub) // a file from outside the shell
				}
			}
			for _, target := range targets {
				if err := s.src.InstructionDrafts.CheckInstructionDraft(ctx, sh.projID, sh.ticketID, target); err != nil {
					return refuseErr(err)
				}
			}
			if row, ok := sh.files[rel]; ok && row.Kind == ports.ShellInstructionDraft {
				if ans := s.stale(sh, row); !ans.Allow {
					return ans
				}
			}
		}
		sh.hold(rel)
	}
	return allow
}

// commitInstructionDrafts stores the edit a session made in the draft: the
// bytes it wrote are the draft's, a file it removed is removed, and then the
// shells of the task are brought up to date. sh is held.
func (s *Service) commitInstructionDrafts(ctx context.Context, sh *root, ed edit) ports.ShellAnswer {
	// A file the session held changed in the draft since it was given: the
	// write is refused, and the shell's copy refreshed.
	for i, rel := range ed.rel {
		if _, ok := inInstructionDrafts(rel); !ok || !ed.in[i] || ed.op == "mkdir" {
			continue
		}
		for _, row := range sh.under(rel, ports.ShellInstructionDraft) {
			if now := s.draftVersion(sh, row); now != row.Version && ed.op != "rm" {
				return refuseStale(row.Path, now)
			}
		}
	}
	var err error
	switch ed.op {
	case "write":
		err = s.storeDraftTree(ctx, sh, ed.rel[len(ed.rel)-1])
	case "rm":
		err = s.dropDraftRows(ctx, sh, ed.rel[0])
	case "mv":
		src, dst := ed.rel[0], ed.rel[1]
		if ed.in[0] {
			err = s.dropDraftRows(ctx, sh, src)
		}
		if err == nil && ed.in[1] {
			err = s.storeDraftTree(ctx, sh, dst)
		}
	}
	if err != nil {
		return refuseErr(err)
	}
	if ans := sh.saveIndex(); !ans.Allow {
		return ans
	}
	s.refreshLocked(sh)
	s.changed(sh.projID, sh.ticketID)
	return allow
}

// storeDraftTree stores the file at rel, or every file in the folder rel, as
// they are on disk. Nothing there stores nothing.
func (s *Service) storeDraftTree(ctx context.Context, sh *root, rel string) error {
	return filepath.WalkDir(sh.abs(rel), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() || strings.HasPrefix(d.Name(), ".harness-tmp-") {
			return nil
		}
		sub, ok := inInstructionDrafts(rel + strings.TrimPrefix(filepath.ToSlash(p), filepath.ToSlash(sh.abs(rel))))
		if !ok {
			return nil
		}
		content, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return s.src.InstructionDrafts.PutInstructionDraft(ctx, sh.projID, sh.ticketID, sub, content)
	})
}

// dropDraftRows removes from the draft the files the session held under rel
// that are gone from disk.
func (s *Service) dropDraftRows(ctx context.Context, sh *root, rel string) error {
	for _, row := range sh.under(rel, ports.ShellInstructionDraft) {
		if _, err := os.Lstat(sh.abs(row.Path)); err == nil {
			continue // the removal did not happen
		}
		sub, _ := inInstructionDrafts(row.Path)
		if err := s.src.InstructionDrafts.RemoveInstructionDraft(ctx, sh.projID, sh.ticketID, sub); err != nil {
			return err
		}
		delete(sh.files, row.Path)
	}
	return nil
}

// draftVersion is the version of the draft file row holds, as stored now,
// "deleted" once it is gone.
func (s *Service) draftVersion(sh *root, row ports.ShellFile) string {
	if s.src.InstructionDrafts == nil {
		return row.Version
	}
	files, err := s.src.InstructionDrafts.ListInstructionDrafts(context.Background(), sh.projID, sh.ticketID)
	if err != nil {
		return row.Version
	}
	for _, f := range files {
		if instructionDraftsDir+"/"+f.Repository+"/"+f.Skill+"/"+f.File == row.Path {
			return Version(f.Content)
		}
	}
	return "deleted"
}

// instructionDrafts adds the task's draft files to the shell, fixed at their
// paths, each writable unless its skill waits on the person.
func (b *builder) instructionDrafts(files []ports.InstructionDraftFile, at time.Time) {
	for _, f := range files {
		meta := ports.ShellFile{
			Path: instructionDraftsDir + "/" + f.Repository + "/" + f.Skill + "/" + f.File, Kind: ports.ShellInstructionDraft,
			Scope: "task", Title: f.File, UpdatedAt: at, Repository: f.Repository, Skill: f.Skill, Writable: f.Writable,
		}
		meta.Version = Version(f.Content)
		b.files = append(b.files, &file{meta: meta, content: f.Content, readOnly: !f.Writable})
	}
}
