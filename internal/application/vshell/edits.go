package vshell

import (
	"context"
	"errors"
	"html"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// What agents do to their shells' files, checked before and stored after,
// through the edit proxy (contract §4–6). Documents are stored here;
// comments and designs in comment_edits.go and design_edits.go.

var _ ports.ShellEdits = (*Service)(nil)

// pendingFor is how long a checked path is kept from refreshes while its
// commit is awaited: a refresh then would overwrite the agent's write
// before it is stored, and lose it.
const pendingFor = time.Minute

// The shell's areas, by what a write there means.
type area int

const (
	outside     area = iota // not in this shell: none of the harness's business
	layout                  // the shell's own folders
	readOnly                // README, index, task.json, resources, attached.md, urls.md
	taskDocs                // task/docs/…: task documents
	projDocs                // project/docs/…: project documents
	taskDesigns             // task/designs/…: the task's designs
	projDesigns             // project/designs/…: project assets, changed only by mv
	comments                // task/comments/…: comments
	unstored                // anywhere else in the shell
)

var layoutDirs = map[string]bool{
	"": true, "task": true, "project": true, "task/docs": true, "task/comments": true,
	"task/resources": true, "task/designs": true, "project/docs": true, "project/designs": true,
}

var readOnlyFiles = map[string]bool{
	"README.md": true, indexFile: true, "task/task.json": true,
	"task/designs/attached.md": true, "task/designs/urls.md": true, "project/designs/urls.md": true,
}

func classify(rel string) area {
	switch {
	case readOnlyFiles[rel] || strings.HasPrefix(rel, "task/resources/"):
		return readOnly
	case layoutDirs[rel]:
		return layout
	case strings.HasPrefix(rel, "task/docs/"):
		return taskDocs
	case strings.HasPrefix(rel, "project/docs/"):
		return projDocs
	case strings.HasPrefix(rel, "task/designs/"):
		return taskDesigns
	case strings.HasPrefix(rel, "project/designs/"):
		return projDesigns
	case strings.HasPrefix(rel, "task/comments/"):
		return comments
	}
	return unstored
}

func (a area) docs() bool { return a == taskDocs || a == projDocs }

func (a area) designs() bool { return a == taskDesigns || a == projDesigns }

func (a area) scope() domain.DocumentScope {
	if a == taskDocs {
		return domain.DocumentScopeTask
	}
	return domain.DocumentScopeProject
}

// edit is one operation resolved against a shell: its kind and its paths
// relative to the shell ("" for the shell itself), each with whether it is
// inside.
type edit struct {
	op  string // write, rm, mv or mkdir
	rel []string
	in  []bool
}

func (e edit) anyInside() bool {
	for _, in := range e.in {
		if in {
			return true
		}
	}
	return false
}

// resolve normalises e's op and places its paths in sh. ok is false for an
// op the proxy does not know.
func (sh *shell) resolve(e ports.ShellEdit) (edit, bool) {
	var out edit
	paths := e.Paths
	switch strings.ToLower(e.Op) {
	case "write", "create", "modify", "edit", "touch":
		out.op = "write"
	case "cp", "copy":
		out.op = "write"
		if len(paths) > 1 {
			paths = paths[len(paths)-1:]
		}
	case "rm", "remove", "delete", "unlink":
		out.op = "rm"
	case "mv", "move", "rename":
		out.op = "mv"
	case "mkdir":
		out.op = "mkdir"
	default:
		out.op = e.Op
	}
	for _, p := range paths {
		rel, in := sh.rel(p)
		out.rel, out.in = append(out.rel, rel), append(out.in, in)
	}
	switch out.op {
	case "write", "rm", "mkdir":
		return out, true
	case "mv":
		return out, len(out.rel) == 2
	}
	return out, false
}

// rel is p relative to the shell, slash separated, and whether p is in it.
// p must be absolute; the shell is matched as made and with its symlinks
// resolved.
func (sh *shell) rel(p string) (string, bool) {
	if !filepath.IsAbs(p) {
		return "", false
	}
	p = filepath.Clean(p)
	for _, base := range []string{sh.dir, sh.realDir} {
		if base == "" {
			continue
		}
		if p == base {
			return "", true
		}
		if strings.HasPrefix(p, base+string(filepath.Separator)) {
			return filepath.ToSlash(p[len(base)+1:]), true
		}
	}
	return "", false
}

// The answers.
var allow = ports.ShellAnswer{Allow: true}

func refuse(code, reason string) ports.ShellAnswer {
	return ports.ShellAnswer{Code: code, Reason: code + ": " + reason}
}

func refuseReadOnly(rel string) ports.ShellAnswer {
	return refuse("READ_ONLY", rel+" is read-only (task.json, resources, index, README, attached.md, urls.md)")
}

func refuseStale(rel, now string) ports.ShellAnswer {
	return refuse("BASE_VERSION_STALE", rel+" changed since this session last received it (now "+now+"); the file has been refreshed — read it again and redo the change")
}

func refuseNotOnTask(rel string) ports.ShellAnswer {
	return refuse("NOT_ON_TASK", rel+" cannot move to task scope: the document is not linked to this task")
}

// refuseUnstored answers a write the harness would not store, so it is not
// made at all rather than silently lost.
func refuseUnstored(rel string, a area) ports.ShellAnswer {
	switch a {
	case projDesigns:
		return refuse("NOT_STORED", rel+" is a project design: it changes only by mv to task/designs and back; nothing was written")
	case comments:
		return refuse("NOT_STORED", rel+": a comment is a file directly in task/comments, written, edited or removed; nothing was written")
	case layout:
		return refuse("NOT_STORED", rel+" is one of the shell's own folders; nothing was written")
	}
	return refuse("NOT_STORED", rel+" is outside the folders the harness stores (task/docs, project/docs, task/comments, task/designs); nothing was written")
}

func refuseCrossMove(src, dst string) ports.ShellAnswer {
	return refuse("NOT_STORED", src+" cannot move to "+dst+": documents move between task/docs and project/docs, designs between task/designs and project/designs; nothing was written")
}

func refuseErr(err error) ports.ShellAnswer {
	code := errs.Code(err)
	if code == "" {
		code = "SHELL_ERROR"
	}
	var se *domain.StructuredError
	if errors.As(err, &se) {
		return refuse(code, se.Message)
	}
	return refuse(code, err.Error())
}

// CheckShellEdit says whether the session may make e. A document path
// whose base version is no longer the stored one is BASE_VERSION_STALE,
// and the shell's copy is refreshed. Paths it allows are kept from
// refreshes until the commit.
func (s *Service) CheckShellEdit(_ context.Context, sessionID string, e ports.ShellEdit) ports.ShellAnswer {
	return s.withShell(sessionID, e, func(sh *shell, ed edit) ports.ShellAnswer {
		return s.check(sh, ed)
	})
}

// CommitShellEdit stores e as the session made it, reading the bytes from
// disk. A path whose operation did not happen (the file it wrote is not
// there, the file it removed still is) stores nothing.
func (s *Service) CommitShellEdit(_ context.Context, sessionID string, e ports.ShellEdit) ports.ShellAnswer {
	return s.withShell(sessionID, e, func(sh *shell, ed edit) ports.ShellAnswer {
		for _, rel := range ed.rel {
			sh.release(rel)
		}
		if sh.skipped {
			s.kick(sh)
		}
		if ans := refuseArea(ed); !ans.Allow {
			return ans
		}
		return s.commit(sh, ed)
	})
}

// withShell runs do on the session's shell, held, for an edit with a path
// inside it; anything else is allowed with nothing said.
func (s *Service) withShell(sessionID string, e ports.ShellEdit, do func(*shell, edit) ports.ShellAnswer) ports.ShellAnswer {
	sh := s.shell(sessionID)
	if sh == nil {
		return allow
	}
	sh.mu.Lock()
	defer sh.mu.Unlock()
	if sh.closed {
		return allow
	}
	ed, ok := sh.resolve(e)
	if !ed.anyInside() {
		return allow
	}
	if !ok {
		return refuse("INVALID_INPUT", "op must be write, rm, mv (source and destination) or mkdir, not "+e.Op)
	}
	return do(sh, ed)
}

// refuseArea refuses an edit of a read-only path, or of one whose writes
// are not stored.
func refuseArea(ed edit) ports.ShellAnswer {
	if ed.op == "mv" {
		return refuseMoveArea(ed)
	}
	for i, rel := range ed.rel {
		if ed.in[i] {
			if ans := refuseOpArea(ed.op, rel); !ans.Allow {
				return ans
			}
		}
	}
	return allow
}

// refuseOpArea: what op may do at rel. Documents, task designs and
// comments directly in task/comments are written and removed; folders are
// made under docs and designs.
func refuseOpArea(op, rel string) ports.ShellAnswer {
	a := classify(rel)
	switch {
	case a == readOnly:
		return refuseReadOnly(rel)
	case op == "mkdir":
		if a == comments || a == unstored {
			return refuseUnstored(rel, a)
		}
		return allow
	case a.docs() || a == taskDesigns:
		return allow
	case a == comments && path.Dir(rel) == "task/comments":
		return allow
	}
	return refuseUnstored(rel, a)
}

// refuseMoveArea: a move into or out of the shell writes or removes; one
// within it stays among documents or among designs.
func refuseMoveArea(ed edit) ports.ShellAnswer {
	src, dst := ed.rel[0], ed.rel[1]
	switch {
	case !ed.in[0]:
		return refuseOpArea("write", dst)
	case !ed.in[1]:
		return refuseOpArea("rm", src)
	}
	from, to := classify(src), classify(dst)
	switch {
	case from == readOnly:
		return refuseReadOnly(src)
	case to == readOnly:
		return refuseReadOnly(dst)
	case from.docs() && to.docs(), from.designs() && to.designs():
		return allow
	case from == to:
		return refuseUnstored(src, from)
	}
	return refuseCrossMove(src, dst)
}

// check is CheckShellEdit with sh held: the paths' areas, then the
// session's copies of what the edit touches.
func (s *Service) check(sh *shell, ed edit) ports.ShellAnswer {
	if ans := refuseArea(ed); !ans.Allow {
		return ans
	}
	switch ed.op {
	case "write", "rm":
		for i, rel := range ed.rel {
			if ed.in[i] {
				if ans := s.checkAt(sh, rel); !ans.Allow {
					return ans
				}
			}
		}
	case "mv":
		src, dst := ed.rel[0], ed.rel[1]
		switch {
		case !ed.in[0]:
			return s.checkAt(sh, dst)
		case !ed.in[1]:
			return s.checkAt(sh, src)
		case classify(src).designs():
			return s.checkDesignMove(sh, src, dst)
		}
		return s.checkMove(sh, src, dst)
	}
	return allow
}

// checkAt: what is at rel, or in the folder rel, may be written or removed.
func (s *Service) checkAt(sh *shell, rel string) ports.ShellAnswer {
	switch classify(rel) {
	case comments:
		return s.checkCommentAt(sh, rel)
	case taskDesigns:
		return s.checkDesignsAt(sh, rel)
	}
	return s.checkDocsAt(sh, rel)
}

// checkDocsAt: the documents at rel, or in the folder rel, may be written
// or removed while the session's copies are the stored ones; a new file
// becomes a document.
func (s *Service) checkDocsAt(sh *shell, rel string) ports.ShellAnswer {
	for _, row := range sh.under(rel, ports.ShellDocument) {
		if ans := s.stale(sh, row); !ans.Allow {
			return ans
		}
	}
	sh.hold(rel)
	return allow
}

// checkMove: between task/docs and project/docs a document changes scope,
// to task scope only when linked to this task; within a folder it is only
// renamed.
func (s *Service) checkMove(sh *shell, src, dst string) ports.ShellAnswer {
	for _, row := range sh.under(src, ports.ShellDocument) {
		if ans := s.stale(sh, row); !ans.Allow {
			return ans
		}
		if classify(dst) == taskDocs && row.Scope != string(domain.DocumentScopeTask) && !s.onTask(sh, row.ID) {
			return refuseNotOnTask(row.Path)
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

// stale answers BASE_VERSION_STALE, and refreshes the shell, when the
// stored document, comment or design is no longer the version row was
// given at.
func (s *Service) stale(sh *shell, row ports.ShellFile) ports.ShellAnswer {
	now := s.storedVersion(sh, row)
	if now == row.Version {
		return allow
	}
	sh.release(row.Path)
	s.refreshLocked(sh)
	return refuseStale(row.Path, now)
}

// storedVersion is the version of what row holds as stored now, "deleted"
// once it is gone.
func (s *Service) storedVersion(sh *shell, row ports.ShellFile) string {
	switch row.Kind {
	case ports.ShellDocument:
		if d := s.src.Writes.GetDocument(row.ID); d != nil {
			return domain.DocumentVersion(d.Content)
		}
	case ports.ShellComment:
		if c := s.comment(sh, row.ID); c != nil {
			return Version([]byte(c.Body))
		}
	case ports.ShellArtifact:
		if a := s.design(row.ID); a != nil {
			return strconv.Itoa(a.Revision)
		}
	default:
		return row.Version
	}
	return "deleted"
}

func (s *Service) onTask(sh *shell, docID string) bool {
	for _, d := range s.src.Documents.ListTicketDocuments(sh.ticketID) {
		if d.ID == docID {
			return true
		}
	}
	return false
}

// commit stores ed, checked; sh is held.
func (s *Service) commit(sh *shell, ed edit) ports.ShellAnswer {
	switch ed.op {
	case "write":
		for i, rel := range ed.rel {
			if ed.in[i] {
				if ans := s.commitWrite(sh, rel); !ans.Allow {
					return ans
				}
			}
		}
	case "rm":
		for i, rel := range ed.rel {
			if ed.in[i] {
				if ans := s.commitRemove(sh, rel); !ans.Allow {
					return ans
				}
			}
		}
	case "mv":
		src, dst := ed.rel[0], ed.rel[1]
		switch {
		case !ed.in[0]:
			return s.commitWrite(sh, dst)
		case !ed.in[1]:
			return s.commitRemove(sh, src)
		case classify(src).designs():
			return s.commitDesignMove(sh, src, dst)
		}
		return s.commitMove(sh, src, dst)
	}
	return allow
}

// commitWrite stores the file written at rel.
func (s *Service) commitWrite(sh *shell, rel string) ports.ShellAnswer {
	switch classify(rel) {
	case comments:
		return s.commitCommentWrite(sh, rel)
	case taskDesigns:
		return s.commitDesignWrite(sh, rel)
	}
	return s.commitDocWrite(sh, rel)
}

// commitRemove stores the removal of what was at rel, or in the folder rel.
func (s *Service) commitRemove(sh *shell, rel string) ports.ShellAnswer {
	switch classify(rel) {
	case comments:
		return s.commitCommentRemove(sh, rel)
	case taskDesigns:
		return s.commitDesignRemove(sh, rel)
	}
	return s.commitDocRemove(sh, rel)
}

// commitDocWrite stores the file at rel: the document's new content, or a
// new document linked to the task. The file keeps the name the agent gave
// it.
func (s *Service) commitDocWrite(sh *shell, rel string) ports.ShellAnswer {
	content, err := os.ReadFile(sh.abs(rel))
	if err != nil || !classify(rel).docs() {
		return allow // nothing was written, or nothing to store
	}
	format := DocumentFormatOf(rel)
	row, ok := sh.files[rel]
	if !ok {
		return s.createDocument(sh, rel, string(content))
	}
	version := domain.DocumentVersion(string(content))
	d := s.src.Writes.GetDocument(row.ID)
	if d == nil {
		return s.stale(sh, row)
	}
	if version == row.Version && d.Format == format {
		return allow
	}
	d, err = s.src.Writes.WriteDocumentIfVersion(row.ID, row.Version, d.Title, string(content), format)
	if errs.HasCode(err, "DOCUMENT_CHANGED") || errs.HasCode(err, "DOCUMENT_NOT_FOUND") {
		return s.stale(sh, row)
	}
	if err != nil {
		return refuseErr(err)
	}
	return sh.record(rel, d)
}

func (s *Service) createDocument(sh *shell, rel, content string) ports.ShellAnswer {
	d, err := s.src.Writes.CreateDocument(sh.projID, TitleOf(rel, content), content, DocumentFormatOf(rel), classify(rel).scope())
	if err != nil {
		return refuseErr(err)
	}
	if err := s.src.Writes.LinkDocument(sh.ticketID, d.ID); err != nil {
		return refuseErr(err)
	}
	return sh.record(rel, d)
}

// commitDocRemove deletes the documents whose files under rel are gone.
func (s *Service) commitDocRemove(sh *shell, rel string) ports.ShellAnswer {
	for _, row := range sh.under(rel, ports.ShellDocument) {
		if _, err := os.Lstat(sh.abs(row.Path)); err == nil {
			continue // the removal did not happen
		}
		if d := s.src.Writes.GetDocument(row.ID); d != nil && domain.DocumentVersion(d.Content) != row.Version {
			return s.stale(sh, row)
		}
		if err := s.src.Writes.DeleteDocument(row.ID); err != nil && !errs.HasCode(err, "DOCUMENT_NOT_FOUND") {
			return refuseErr(err)
		}
		delete(sh.files, row.Path)
	}
	return sh.saveIndex()
}

// commitMove moves the documents under src to dst, once they are there:
// scope follows the folder, format the extension; a document the move
// wrote over is deleted.
func (s *Service) commitMove(sh *shell, src, dst string) ports.ShellAnswer {
	rows := sh.under(src, ports.ShellDocument)
	if len(rows) == 0 {
		return s.commitDocWrite(sh, dst)
	}
	for _, row := range rows {
		to := moved(row.Path, src, dst)
		if _, err := os.Lstat(sh.abs(to)); err != nil {
			continue // the move did not happen
		}
		if over, ok := sh.files[to]; ok && over.ID != row.ID {
			if err := s.src.Writes.DeleteDocument(over.ID); err != nil && !errs.HasCode(err, "DOCUMENT_NOT_FOUND") {
				return refuseErr(err)
			}
			delete(sh.files, to)
		}
		d := s.src.Writes.GetDocument(row.ID)
		if d == nil || domain.DocumentVersion(d.Content) != row.Version {
			return s.stale(sh, row)
		}
		scope := classify(to).scope()
		if scope != d.Scope {
			if scope == domain.DocumentScopeTask && !s.onTask(sh, d.ID) {
				delete(sh.files, row.Path)
				_ = os.Remove(sh.abs(to))
				s.refreshLocked(sh)
				return refuseNotOnTask(row.Path)
			}
			var err error
			if d, err = s.src.Writes.SetDocumentScope(d.ID, scope); err != nil {
				return refuseErr(err)
			}
		}
		if format := DocumentFormatOf(to); format != d.Format {
			var err error
			if d, err = s.src.Writes.WriteDocumentIfVersion(d.ID, row.Version, d.Title, d.Content, format); err != nil {
				return refuseErr(err)
			}
		}
		delete(sh.files, row.Path)
		sh.files[to] = docRow(to, d)
	}
	return sh.saveIndex()
}

// moved is p, under src, at its place under dst.
func moved(p, src, dst string) string {
	if p == src {
		return dst
	}
	return dst + strings.TrimPrefix(p, src)
}

// under is the rows of kind at rel or in the folder rel.
func (sh *shell) under(rel, kind string) []ports.ShellFile {
	var out []ports.ShellFile
	for p, row := range sh.files {
		if row.Kind == kind && (p == rel || strings.HasPrefix(p, rel+"/")) {
			out = append(out, row)
		}
	}
	return out
}

// record makes d, stored from the file at rel, the session's base version
// there, before any refresh can place it elsewhere.
func (sh *shell) record(rel string, d *domain.Document) ports.ShellAnswer {
	sh.files[rel] = docRow(rel, d)
	return sh.saveIndex()
}

func (sh *shell) saveIndex() ports.ShellAnswer {
	if err := sh.writeIndex(); err != nil {
		return refuseErr(err)
	}
	return allow
}

func docRow(rel string, d *domain.Document) ports.ShellFile {
	return ports.ShellFile{Path: rel, Kind: ports.ShellDocument, ID: d.ID, Scope: string(d.Scope),
		Version: domain.DocumentVersion(d.Content), Title: d.Title, UpdatedAt: d.UpdatedAt}
}

// restore puts back what is stored at rel after a refused commit, so the
// shell never shows what was not stored: a file the harness has no record
// of is removed, one it has is rewritten by the refresh.
func (s *Service) restore(sh *shell, rel string) {
	sh.release(rel)
	if row, ok := sh.files[rel]; ok {
		row.Version, row.UpdatedAt = "", time.Time{}
		sh.files[rel] = row
	} else {
		_ = os.Remove(sh.abs(rel))
	}
	s.refreshLocked(sh)
}

// hold keeps rel, and the files under it, from refreshes until its commit
// or pendingFor.
func (sh *shell) hold(rel string) {
	if sh.pending == nil {
		sh.pending = map[string]time.Time{}
	}
	sh.pending[rel] = time.Now()
}

func (sh *shell) release(rel string) { delete(sh.pending, rel) }

// held says a refresh must leave p as it is.
func (sh *shell) held(p string) bool {
	for rel, at := range sh.pending {
		if time.Since(at) < pendingFor && (p == rel || strings.HasPrefix(p, rel+"/")) {
			return true
		}
	}
	return false
}

// TitleOf is a new document's title: its first heading (Markdown #, HTML
// <h1> or <title>), else the file's name without its extension.
func TitleOf(rel, content string) string {
	best, at := "", -1
	if loc := mdHeading.FindStringSubmatchIndex(content); loc != nil {
		best, at = content[loc[2]:loc[3]], loc[0]
	}
	for _, re := range []*regexp.Regexp{htmlH1, htmlTitle} {
		if loc := re.FindStringSubmatchIndex(content); loc != nil && (at < 0 || loc[0] < at) {
			best, at = content[loc[2]:loc[3]], loc[0]
		}
	}
	if at >= 0 {
		t := strings.Join(strings.Fields(html.UnescapeString(tags.ReplaceAllString(best, ""))), " ")
		t = strings.TrimSpace(strings.TrimRight(t, "#"))
		if t != "" {
			return t
		}
	}
	name := path.Base(rel)
	if t := strings.TrimSuffix(name, path.Ext(name)); t != "" {
		return t
	}
	return name
}

var (
	mdHeading = regexp.MustCompile(`(?m)^ {0,3}#{1,6}[ \t]+(.+?)[ \t]*$`)
	htmlH1    = regexp.MustCompile(`(?is)<h1\b[^>]*>(.*?)</h1>`)
	htmlTitle = regexp.MustCompile(`(?is)<title\b[^>]*>(.*?)</title>`)
	tags      = regexp.MustCompile(`<[^>]*>`)
)
