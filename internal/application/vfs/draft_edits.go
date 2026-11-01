package vfs

import (
	"context"
	"os"
	"path"
	"strings"

	"operators-mcp/internal/ports"
)

// What a design session does to its draft, project/design-system/ in its
// shell: every file but README.md is writable, and a write is checked and
// stored as the design system's draft, never as a document.

// inDraft says rel is in the draft's folder, and its path there.
func inDraft(rel string) (string, bool) {
	if rel == designSystemDir {
		return "", true
	}
	return strings.CutPrefix(rel, designSystemDir+"/")
}

// touchesDraft says the shell is a design session's and ed has a path in its
// draft.
func (sh *root) touchesDraft(ed edit) bool {
	if !sh.draft {
		return false
	}
	for i, rel := range ed.rel {
		if _, ok := inDraft(rel); ok && ed.in[i] {
			return true
		}
	}
	return false
}

// checkDraft is the check of an edit that touches the draft: what a design
// session may write there, while its proposal does not wait on the person.
func (s *Service) checkDraft(ctx context.Context, sh *root, ed edit) ports.ShellAnswer {
	if ed.op == "mv" {
		return refuse("READ_ONLY", "a draft file is not moved: write the new file and rm the old one")
	}
	for i, rel := range ed.rel {
		sub, ok := inDraft(rel)
		if !ok || !ed.in[i] {
			continue
		}
		if ans := draftPath(ed.op, sub); !ans.Allow {
			return ans
		}
		if ed.op == "mkdir" {
			continue
		}
		if view, err := s.src.Drafts.DesignDraftFiles(ctx, sh.sessionID, sh.boundDS); err == nil && view != nil && view.Submitted {
			return refuse("PROPOSAL_SUBMITTED", "your proposal waits on the person; it returns to you if they send items back")
		}
		sh.hold(rel)
	}
	return allow
}

// draftPath answers what op may do at sub, a path in the draft's folder.
func draftPath(op, sub string) ports.ShellAnswer {
	switch {
	case op == "mkdir":
		if sub == "" || sub == "components" {
			return allow
		}
		return refuse("READ_ONLY", designSystemDir+"/"+sub+": the draft holds only tokens.json, DESIGN.md and components/<slug>.html")
	case sub == "README.md":
		return refuse("READ_ONLY", designSystemDir+"/README.md is generated: edit tokens.json, DESIGN.md's body or components/<slug>.html")
	case sub == "tokens.json" || sub == "DESIGN.md":
		return allow
	case path.Dir(sub) == "components" && strings.HasSuffix(sub, ".html") && len(path.Base(sub)) > len(".html"):
		return allow
	}
	return refuse("READ_ONLY", designSystemDir+"/"+sub+": the draft holds only tokens.json, DESIGN.md and components/<slug>.html")
}

// commitDraft stores the edit a design session made in its draft: the bytes
// it wrote are the draft's, when the draft takes them, and then every file of
// the draft is made again from it, so the session reads back what would be
// exported. sh is held.
func (s *Service) commitDraft(ctx context.Context, sh *root, ed edit) ports.ShellAnswer {
	for i, rel := range ed.rel {
		sub, ok := inDraft(rel)
		if !ok || !ed.in[i] || sub == "" || ed.op == "mkdir" {
			continue
		}
		var err error
		switch ed.op {
		case "write":
			var content []byte
			content, err = os.ReadFile(sh.abs(rel))
			if os.IsNotExist(err) {
				continue // nothing was written
			}
			if err == nil {
				err = s.src.Drafts.WriteDraftFile(ctx, sh.sessionID, sh.boundDS, sub, content)
			}
		case "rm":
			if _, statErr := os.Lstat(sh.abs(rel)); statErr == nil {
				continue // the file is still there: nothing was removed
			}
			err = s.src.Drafts.RemoveDraftFile(ctx, sh.sessionID, sh.boundDS, sub)
		}
		if err != nil {
			return refuseErr(err)
		}
	}
	sh.unstore(designSystemDir)
	return allow
}
