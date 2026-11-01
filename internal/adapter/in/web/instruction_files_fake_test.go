package web

import (
	"encoding/json"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// fakeInstructionFiles answers the endpoints of "Contract: Harness server ↔
// Web UI — instruction files" the way the server does, as far as the browser
// can tell: versions and INSTRUCTION_CHANGED, INSTRUCTION_EXISTS, path
// validation with details.errors, import from a settable list of repository
// copies, removal from the repository with its refusals, and the project
// feed's instruction_file_changed event. The browser tests of the Instructions
// tab run against it. It belongs to one repository, "r1".
type fakeInstructionFiles struct {
	mu    sync.Mutex
	feed  *sseFeed
	files map[string]*fakeInstructionFile
	// copies are the instruction-like files found in the checkout's default
	// branch, in the order the list answers them. Setting them is setCopies.
	copies []fakeRepositoryCopy
	// refusals are the errors the next call of an endpoint answers with, by
	// endpoint name (list_instruction_files, …); each is used once.
	refusals map[string]fakeRefusal
	// calls are the endpoints asked, in order; bodies the request body of each.
	calls  []string
	bodies []map[string]any
	// commits are the commits remove_instruction_files_from_repository made.
	commits []fakeCommit
	// saveDelay holds a save's answer back after it is stored and announced.
	saveDelay time.Duration
	// onDeleted is told the path of each file a delete_instruction_file removed, as the server also removes the row's record entry (the contract).
	onDeleted func(path string)
	// clock is the number of the next version stamp, so updated_at moves on.
	clock int
}

type fakeInstructionFile struct {
	Kind    string
	Version int
	Content string
	Stamp   time.Time
}

// fakeRepositoryCopy is a file in the checkout.
type fakeRepositoryCopy struct {
	Path, Kind, Content string
}

type fakeRefusal struct {
	Status  int
	Code    string
	Message string
	Details any
}

type fakeCommit struct {
	Hash  string
	Paths []string
}

const (
	instructionMaxBytes   = 256 * 1024
	fakeInstructionCommit = "abc1234def"
	removeCommitMessage   = "chore(architecture): move instruction files into the harness"
)

func newFakeInstructionFiles(feed *sseFeed) *fakeInstructionFiles {
	return &fakeInstructionFiles{feed: feed, files: map[string]*fakeInstructionFile{}, refusals: map[string]fakeRefusal{}}
}

var instructionRoutes = []string{
	"list_instruction_files", "get_instruction_file", "save_instruction_file", "rename_instruction_file",
	"delete_instruction_file", "import_instruction_files", "remove_instruction_files_from_repository",
}

// register mounts the seven endpoints on mux.
func (f *fakeInstructionFiles) register(mux *http.ServeMux) {
	for _, r := range instructionRoutes {
		mux.Handle("/api/"+r, f)
	}
}

// seed stores a file as if the person had saved it earlier; it announces nothing.
func (f *fakeInstructionFiles) seed(path, kind, content string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.put(path, kind, content)
}

func (f *fakeInstructionFiles) put(path, kind, content string) *fakeInstructionFile {
	f.clock++
	cur := f.files[path]
	if cur == nil {
		cur = &fakeInstructionFile{}
		f.files[path] = cur
	}
	cur.Kind, cur.Content, cur.Stamp = kind, content, time.Date(2026, 10, 31, 9, 0, f.clock, 0, time.UTC)
	cur.Version++
	return cur
}

// setCopies replaces the files the checkout's default branch holds.
func (f *fakeInstructionFiles) setCopies(copies ...fakeRepositoryCopy) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.copies = copies
}

// refuseNext makes the next call of an endpoint fail with the code.
func (f *fakeInstructionFiles) refuseNext(endpoint string, status int, code string, details any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refusals[endpoint] = fakeRefusal{Status: status, Code: code, Message: "refused: " + code, Details: details}
}

// saveElsewhere is a save made by another tab: the version moves on and the
// project feed announces it. A path the harness does not have is created.
func (f *fakeInstructionFiles) saveElsewhere(path, content string) {
	f.mu.Lock()
	kind := "memory"
	if cur := f.files[path]; cur != nil {
		kind = cur.Kind
	}
	v := f.put(path, kind, content).Version
	f.mu.Unlock()
	f.emitChanged(path, v)
}

// deleteElsewhere is a delete made by another tab.
func (f *fakeInstructionFiles) deleteElsewhere(path string) {
	f.mu.Lock()
	delete(f.files, path)
	f.mu.Unlock()
	f.emitChanged(path, 0)
}

// emitChanged sends instruction_file_changed to every follower of the project feed.
func (f *fakeInstructionFiles) emitChanged(path string, version int) {
	b, _ := json.Marshal(map[string]any{"instruction_file_changed": map[string]any{"repository_id": "r1", "path": path, "version": version}})
	f.feed.mu.Lock()
	defer f.feed.mu.Unlock()
	for _, c := range f.feed.conns {
		c <- b
	}
}

func (f *fakeInstructionFiles) content(path string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cur := f.files[path]
	if cur == nil {
		return "", false
	}
	return cur.Content, true
}

func (f *fakeInstructionFiles) version(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	if cur := f.files[path]; cur != nil {
		return cur.Version
	}
	return 0
}

func (f *fakeInstructionFiles) called() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// lastBody is the request body of the newest call of an endpoint, or nil.
func (f *fakeInstructionFiles) lastBody(endpoint string) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.calls) - 1; i >= 0; i-- {
		if f.calls[i] == endpoint {
			return f.bodies[i]
		}
	}
	return nil
}

func (f *fakeInstructionFiles) madeCommits() []fakeCommit {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeCommit(nil), f.commits...)
}

func (f *fakeInstructionFiles) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var in map[string]any
	_ = json.NewDecoder(r.Body).Decode(&in)
	name := strings.TrimPrefix(r.URL.Path, "/api/")
	if name == "save_instruction_file" {
		defer f.delaySaveAnswer() // runs after the lock below is released
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, name)
	f.bodies = append(f.bodies, in)
	if ref, ok := f.refusals[name]; ok {
		delete(f.refusals, name)
		f.fail(w, ref.Status, ref.Code, ref.Message, ref.Details)
		return
	}
	if id, _ := in["repository_id"].(string); id != "r1" {
		f.fail(w, http.StatusNotFound, "REPOSITORY_NOT_FOUND", "repository not found", nil)
		return
	}
	switch name {
	case "list_instruction_files":
		f.list(w)
	case "get_instruction_file":
		f.get(w, in)
	case "save_instruction_file":
		f.save(w, in)
	case "rename_instruction_file":
		f.rename(w, in)
	case "delete_instruction_file":
		f.remove(w, in)
	case "import_instruction_files":
		f.importFiles(w, in)
	case "remove_instruction_files_from_repository":
		f.removeFromRepository(w, in)
	}
}

func (f *fakeInstructionFiles) delaySaveAnswer() {
	f.mu.Lock()
	d := f.saveDelay
	f.mu.Unlock()
	time.Sleep(d)
}

func (f *fakeInstructionFiles) meta(path string, withContent bool) map[string]any {
	cur := f.files[path]
	m := map[string]any{
		"path": path, "kind": cur.Kind, "version": cur.Version, "size": len(cur.Content),
		"updated_at": cur.Stamp.Format(time.RFC3339),
	}
	if withContent {
		m["content"] = cur.Content
	}
	return m
}

func (f *fakeInstructionFiles) paths() []string {
	out := make([]string, 0, len(f.files))
	for p := range f.files {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func (f *fakeInstructionFiles) list(w http.ResponseWriter) {
	files := []any{}
	for _, p := range f.paths() {
		files = append(files, f.meta(p, false))
	}
	copies := []any{}
	for _, c := range f.copies {
		cur := f.files[c.Path]
		copies = append(copies, map[string]any{"path": c.Path, "kind": c.Kind, "differs": cur == nil || cur.Content != c.Content})
	}
	f.json(w, map[string]any{"files": files, "repository_copies": copies})
}

func (f *fakeInstructionFiles) get(w http.ResponseWriter, in map[string]any) {
	path, _ := in["path"].(string)
	if f.files[path] == nil {
		f.fail(w, http.StatusNotFound, "INSTRUCTION_NOT_FOUND", "no instruction file at "+path, nil)
		return
	}
	f.json(w, f.meta(path, true))
}

func (f *fakeInstructionFiles) save(w http.ResponseWriter, in map[string]any) {
	path, _ := in["path"].(string)
	kind, _ := in["kind"].(string)
	content, _ := in["content"].(string)
	base := int(num(in["base_version"]))
	if errs := validateInstructionFile(path, kind, content); len(errs) > 0 {
		f.fail(w, http.StatusBadRequest, "INSTRUCTION_INVALID", "The instruction file breaks validation.", map[string]any{"errors": errs})
		return
	}
	cur := f.files[path]
	switch {
	case base == 0 && cur != nil:
		f.fail(w, http.StatusConflict, "INSTRUCTION_EXISTS", "a file already exists at "+path, nil)
		return
	case base != 0 && cur == nil:
		f.fail(w, http.StatusNotFound, "INSTRUCTION_NOT_FOUND", "no instruction file at "+path, nil)
		return
	case base != 0 && cur.Version != base:
		f.fail(w, http.StatusConflict, "INSTRUCTION_CHANGED", "The file was saved by someone else.", nil)
		return
	}
	v := f.put(path, kind, content).Version
	f.emitLocked(path, v)
	f.json(w, f.meta(path, false))
}

func (f *fakeInstructionFiles) rename(w http.ResponseWriter, in map[string]any) {
	path, _ := in["path"].(string)
	to, _ := in["new_path"].(string)
	base := int(num(in["base_version"]))
	cur := f.files[path]
	if cur == nil {
		f.fail(w, http.StatusNotFound, "INSTRUCTION_NOT_FOUND", "no instruction file at "+path, nil)
		return
	}
	if errs := validateInstructionFile(to, cur.Kind, cur.Content); len(errs) > 0 {
		f.fail(w, http.StatusBadRequest, "INSTRUCTION_INVALID", "The new path breaks validation.", map[string]any{"errors": errs})
		return
	}
	if cur.Version != base {
		f.fail(w, http.StatusConflict, "INSTRUCTION_CHANGED", "The file was saved by someone else.", nil)
		return
	}
	if f.files[to] != nil {
		f.fail(w, http.StatusConflict, "INSTRUCTION_EXISTS", "a file already exists at "+to, nil)
		return
	}
	kind, content := cur.Kind, cur.Content
	delete(f.files, path)
	v := f.put(to, kind, content).Version
	f.emitLocked(path, 0)
	f.emitLocked(to, v)
	f.json(w, f.meta(to, false))
}

func (f *fakeInstructionFiles) remove(w http.ResponseWriter, in map[string]any) {
	path, _ := in["path"].(string)
	base := int(num(in["base_version"]))
	cur := f.files[path]
	if cur == nil {
		f.fail(w, http.StatusNotFound, "INSTRUCTION_NOT_FOUND", "no instruction file at "+path, nil)
		return
	}
	if cur.Version != base {
		f.fail(w, http.StatusConflict, "INSTRUCTION_CHANGED", "The file was saved by someone else.", nil)
		return
	}
	delete(f.files, path)
	if f.onDeleted != nil {
		f.onDeleted(path)
	}
	f.emitLocked(path, 0)
	w.WriteHeader(http.StatusNoContent)
}

func (f *fakeInstructionFiles) copyAt(path string) *fakeRepositoryCopy {
	for i := range f.copies {
		if f.copies[i].Path == path {
			return &f.copies[i]
		}
	}
	return nil
}

func (f *fakeInstructionFiles) importFiles(w http.ResponseWriter, in map[string]any) {
	paths := stringList(in["paths"])
	for _, p := range paths {
		if f.copyAt(p) == nil {
			f.fail(w, http.StatusNotFound, "INSTRUCTION_NOT_FOUND", "the checkout has no file at "+p, nil)
			return
		}
	}
	files := []any{}
	for _, p := range paths {
		c := f.copyAt(p)
		v := f.put(p, c.Kind, c.Content).Version
		f.emitLocked(p, v)
		files = append(files, f.meta(p, false))
	}
	f.json(w, map[string]any{"files": files})
}

func (f *fakeInstructionFiles) removeFromRepository(w http.ResponseWriter, in map[string]any) {
	paths := stringList(in["paths"])
	for _, p := range paths {
		if f.files[p] == nil {
			f.fail(w, http.StatusConflict, "INSTRUCTION_NOT_IMPORTED", p+" is not in the harness yet", nil)
			return
		}
	}
	kept := f.copies[:0:0]
	for _, c := range f.copies {
		if !hasString(paths, c.Path) {
			kept = append(kept, c)
		}
	}
	f.copies = kept
	f.commits = append(f.commits, fakeCommit{Hash: fakeInstructionCommit, Paths: paths})
	f.json(w, map[string]any{"commit": fakeInstructionCommit})
}

// emitLocked announces a change while f.mu is held; emitChanged takes only the feed's lock.
func (f *fakeInstructionFiles) emitLocked(path string, version int) {
	f.emitChanged(path, version)
}

func (f *fakeInstructionFiles) json(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (f *fakeInstructionFiles) fail(w http.ResponseWriter, status int, code, msg string, details any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body := map[string]any{"error": msg, "code": code}
	if details != nil {
		body["details"] = details
	}
	_ = json.NewEncoder(w).Encode(body)
}

var (
	memoryPath   = regexp.MustCompile(`^([^/]+/)*[A-Z]+\.md$`)
	skillPath    = regexp.MustCompile(`^\.claude/skills/[^/]+/.+$`)
	subagentPath = regexp.MustCompile(`^\.claude/agents/[^/]+\.md$`)
)

// validateInstructionFile is the part of the server's validation the browser
// reaches: the path is relative, stays inside the repository, is not in .git
// and fits its kind's pattern, and the content is at most 256 KB. The errors
// use the record contract's form, with JSON pointers into the request body.
func validateInstructionFile(path, kind, content string) []archError {
	var errs []archError
	add := func(ptr, code, msg string) { errs = append(errs, archError{"path": ptr, "code": code, "message": msg}) }
	switch {
	case path == "":
		add("/path", "MISSING_FIELD", "A file needs a path.")
	case strings.HasPrefix(path, "/"):
		add("/path", "ABSOLUTE_PATH", "Use a path inside the repository, not an absolute one.")
	case strings.Contains(path, ".."):
		add("/path", "PATH_ESCAPES", "A path cannot contain ..")
	case path == ".git" || strings.HasPrefix(path, ".git/"):
		add("/path", "PATH_IN_GIT", "A path cannot be inside .git.")
	default:
		fits := map[string]*regexp.Regexp{"memory": memoryPath, "skill": skillPath, "subagent": subagentPath}[kind]
		if fits == nil || !fits.MatchString(path) {
			add("/path", "PATH_KIND_MISMATCH", "The path does not fit a "+kind+" file.")
		}
	}
	if len(content) > instructionMaxBytes {
		add("/content", "TOO_LARGE", "A file can be at most 256 KB.")
	}
	return errs
}

func num(v any) float64 {
	n, _ := v.(float64)
	return n
}

func stringList(v any) []string {
	var out []string
	list, _ := v.([]any)
	for _, x := range list {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func hasString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
