package designsystem

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// A design session edits a draft of the design system it is bound to: a
// working copy of one version that goes through the same validation as a
// save, so a proposal made from it always saves (contract "Harness server ↔
// Agent sessions — design-system view, draft and proposals").

// Draft is a design session's working copy as its shell shows it.
type Draft struct {
	DesignSystemID string
	ProjectID      string
	Identity       Identity
	// BaseVersion is the version the draft started from; At is when that
	// version was saved.
	BaseVersion int
	At          time.Time
	Record      domain.DesignSystemRecord
	// Submitted says the session's proposal is open: the draft waits on the
	// person and takes no writes.
	Submitted bool
	// Changed says the draft differs from its base.
	Changed bool
}

// Files are the files the draft would export if it were saved now. A draft
// with changes shows the next version's number; one without shows its base.
func (d *Draft) Files() []File {
	version := d.BaseVersion
	if d.Changed {
		version++
	}
	return Files(d.Identity, version, d.At, d.Record)
}

// DesignDraftFiles implements ports.DesignDrafts.
func (s *Service) DesignDraftFiles(ctx context.Context, sessionID, designSystemID string) (*ports.DesignDraftView, error) {
	d, err := s.DraftOf(ctx, sessionID, designSystemID)
	if err != nil || d == nil {
		return nil, err
	}
	view := &ports.DesignDraftView{Submitted: d.Submitted}
	for _, f := range d.Files() {
		view.Files = append(view.Files, ports.DesignDraftFile{Path: f.Path, Content: f.Content, Editable: Editable(f.Path)})
	}
	return view, nil
}

// Editable says a file of the draft is one a session may write: README.md is
// generated.
func Editable(rel string) bool { return rel != "README.md" }

// UseSessionReader sets where a session's mode and design system are read.
func (s *Service) UseSessionReader(r interface {
	Get(id string) *domain.Session
}) {
	s.sessionStore = r
}

// IsDesignSession says the session is a design session.
func (s *Service) IsDesignSession(sessionID string) bool {
	if s.sessionStore == nil || s.proposals == nil {
		return false
	}
	sess := s.sessionStore.Get(sessionID)
	return sess != nil && sess.Mode == domain.SessionModeDesignSystem && sess.DesignSystemID != ""
}

// baseRecord is the record of a version, empty for version 0.
func (s *Service) baseRecord(ctx context.Context, designSystemID string, version int) (domain.DesignSystemRecord, error) {
	var rec domain.DesignSystemRecord
	if version > 0 {
		v, err := s.store.Get(ctx, designSystemID, version)
		if err != nil {
			return rec, err
		}
		if v != nil {
			rec = v.Record
		}
	}
	rec.Normalize()
	return rec, nil
}

// DraftOf is the design session's draft: its drafting or open proposal's
// working copy, or the current version when it has made none yet. It answers
// nil for a session that is not a design session.
func (s *Service) DraftOf(ctx context.Context, sessionID, designSystemID string) (*Draft, error) {
	if s.proposals == nil || designSystemID == "" {
		return nil, nil
	}
	s.draftMu.Lock()
	defer s.draftMu.Unlock()
	return s.draftOf(ctx, sessionID, designSystemID)
}

func (s *Service) draftOf(ctx context.Context, sessionID, designSystemID string) (*Draft, error) {
	id, cur, err := s.current(ctx, designSystemID)
	if err != nil {
		return nil, err
	}
	d := &Draft{DesignSystemID: id.ID, ProjectID: id.ProjectID, Identity: Identity{Name: id.Name, Slug: id.Slug, Description: id.Description}}
	p, err := s.proposals.ActiveBySession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if p == nil {
		d.Record = domain.DesignSystemRecord{}
		if cur != nil {
			d.BaseVersion, d.At, d.Record = cur.Version, cur.At, cur.Record
		}
		d.Record.Normalize()
		return d, nil
	}
	d.BaseVersion, d.Record, d.Submitted = p.BaseVersion, p.Draft, p.State == domain.ProposalOpen
	if p.BaseVersion > 0 {
		base, err := s.store.Get(ctx, id.ID, p.BaseVersion)
		if err != nil {
			return nil, err
		}
		if base != nil {
			d.At = base.At
		}
	}
	baseRec, err := s.baseRecord(ctx, id.ID, p.BaseVersion)
	if err != nil {
		return nil, err
	}
	d.Changed = !sameRecord(baseRec, p.Draft)
	return d, nil
}

func sameRecord(a, b domain.DesignSystemRecord) bool {
	a.Normalize()
	b.Normalize()
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

// WriteDraftFile applies the file the design session wrote at rel (relative to
// project/design-system/) to its draft. It refuses a write the draft cannot
// take, with the reason, and then stores nothing.
func (s *Service) WriteDraftFile(ctx context.Context, sessionID, designSystemID, rel string, content []byte) error {
	return s.editDraft(ctx, sessionID, designSystemID, func(d *Draft) (domain.DesignSystemRecord, error) {
		rec := d.Record
		switch {
		case rel == "tokens.json":
			tokens, skipped, err := ParseTokensJSON(content)
			if err != nil {
				return rec, refuse(domain.DesignBadValue, "tokens.json is not readable: "+err.Error())
			}
			if len(skipped) > 0 {
				return rec, refuse(domain.DesignBadValue, "tokens.json has entries the draft cannot take: "+skips(skipped))
			}
			rec.Tokens = tokens
		case rel == "DESIGN.md":
			return s.writeDesignMD(d, content)
		case isComponentFile(rel):
			c, err := ParseComponentHTML(content)
			if err != nil {
				return rec, refuse(domain.DesignBadValue, rel+" is not a readable component page: "+err.Error())
			}
			if want := strings.TrimSuffix(path.Base(rel), ".html"); c.Slug != want {
				return rec, refuse(domain.DesignBadName, fmt.Sprintf("%s holds the component %q: the file name is the slug", rel, c.Slug))
			}
			rec.Components = withComponent(rec.Components, c)
		default:
			return rec, notEditable(rel)
		}
		return rec, nil
	})
}

// RemoveDraftFile applies the removal of rel to the design session's draft: a
// component page removes the component, tokens.json and DESIGN.md clear the
// tokens and the principles.
func (s *Service) RemoveDraftFile(ctx context.Context, sessionID, designSystemID, rel string) error {
	return s.editDraft(ctx, sessionID, designSystemID, func(d *Draft) (domain.DesignSystemRecord, error) {
		rec := d.Record
		switch {
		case rel == "tokens.json":
			rec.Tokens = []domain.DesignToken{}
		case rel == "DESIGN.md":
			rec.Principles = ""
		case isComponentFile(rel):
			slug := strings.TrimSuffix(path.Base(rel), ".html")
			kept := make([]domain.DesignComponent, 0, len(rec.Components))
			for _, c := range rec.Components {
				if c.Slug != slug {
					kept = append(kept, c)
				}
			}
			rec.Components = kept
		default:
			return rec, notEditable(rel)
		}
		return rec, nil
	})
}

// editDraft runs edit on the session's draft and stores what it answers when
// the result is valid by the web contract's validation, as a save would be.
func (s *Service) editDraft(ctx context.Context, sessionID, designSystemID string, edit func(*Draft) (domain.DesignSystemRecord, error)) error {
	if s.proposals == nil || designSystemID == "" {
		return &domain.StructuredError{Code: "NOT_A_DESIGN_SESSION", Message: "only a design session has a design-system draft"}
	}
	s.draftMu.Lock()
	defer s.draftMu.Unlock()
	d, err := s.draftOf(ctx, sessionID, designSystemID)
	if err != nil {
		return err
	}
	if d.Submitted {
		return &domain.StructuredError{Code: "PROPOSAL_SUBMITTED", Message: "your proposal waits on the person; it returns to you if they send items back"}
	}
	rec, err := edit(d)
	if err != nil {
		return err
	}
	rec.Normalize()
	if problems := rec.Validate(d.Record); len(problems) > 0 {
		return invalidDraft(problems)
	}
	p, err := s.proposals.ActiveBySession(ctx, sessionID)
	if err != nil {
		return err
	}
	if p == nil {
		p = &domain.DesignProposal{DesignSystemID: d.DesignSystemID, SessionID: sessionID, State: domain.ProposalDrafting, BaseVersion: d.BaseVersion, Draft: rec}
		if err := s.proposals.Create(ctx, p); err != nil {
			return err
		}
		s.announceProposal(d.ProjectID, p)
		return nil
	}
	p.Draft = rec
	if err := s.proposals.Update(ctx, p); err != nil {
		return err
	}
	s.announceProposal(d.ProjectID, p)
	return nil
}

// writeDesignMD takes DESIGN.md's body as the principles; its frontmatter
// must be the one generated from the draft's tokens.
func (s *Service) writeDesignMD(d *Draft, content []byte) (domain.DesignSystemRecord, error) {
	rec := d.Record
	named, tokens, principles, _, err := ParseDesignMD(content)
	if err != nil {
		return rec, refuse(domain.DesignBadValue, "DESIGN.md is not readable: "+err.Error())
	}
	got, _ := splitFrontmatter(string(content))
	want, _ := splitFrontmatter(designMD(d.Identity, d.BaseVersion+1, d.At.UTC().Format("2006-01-02"), rec))
	// With no tokens either side the frontmatter has nothing to generate but
	// the name and the description, which only the person edits.
	bare := len(tokens) == 0 && len(rec.Tokens) == 0 &&
		(named.Name == "" || named.Name == d.Identity.Name) && (named.Description == "" || named.Description == d.Identity.Description)
	if strings.TrimSpace(got) != strings.TrimSpace(want) && !bare {
		return rec, refuse(domain.DesignBadValue, "edit tokens in tokens.json; DESIGN.md's frontmatter is generated")
	}
	rec.Principles = principles
	return rec, nil
}

func isComponentFile(rel string) bool {
	return path.Dir(rel) == "components" && strings.HasSuffix(rel, ".html") && len(path.Base(rel)) > len(".html")
}

func notEditable(rel string) error {
	if rel == "README.md" {
		return &domain.StructuredError{Code: "READ_ONLY", Message: "README.md is generated: write tokens.json, DESIGN.md or components/<slug>.html"}
	}
	return refuse(domain.DesignBadName, rel+" is not part of the design system: write tokens.json, DESIGN.md or components/<slug>.html")
}

func withComponent(list []domain.DesignComponent, c domain.DesignComponent) []domain.DesignComponent {
	out := make([]domain.DesignComponent, 0, len(list)+1)
	replaced := false
	for _, have := range list {
		if have.Slug == c.Slug {
			out, replaced = append(out, c), true
			continue
		}
		out = append(out, have)
	}
	if !replaced {
		out = append(out, c)
	}
	return out
}

func skips(skipped []skip) string {
	parts := make([]string, len(skipped))
	for i, sk := range skipped {
		parts[i] = sk.Path + " (" + sk.Reason + ")"
	}
	return strings.Join(parts, "; ")
}

func refuse(code, message string) error {
	return invalidDraft([]domain.DesignFieldError{{Path: "/", Code: code, Message: message}})
}

// invalidDraft is DESIGN_SYSTEM_INVALID with each error in the message, which
// is all an agent sees of a refusal.
func invalidDraft(problems []domain.DesignFieldError) error {
	parts := make([]string, len(problems))
	for i, p := range problems {
		parts[i] = fmt.Sprintf("%s %s: %s", p.Path, p.Code, p.Message)
	}
	return &domain.StructuredError{Code: "DESIGN_SYSTEM_INVALID", Message: "the draft would be invalid: " + strings.Join(parts, "; ")}
}
