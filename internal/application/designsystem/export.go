package designsystem

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/pmezard/go-difflib/difflib"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.DesignSystemExports = (*Service)(nil)

// exporter is what an export reads and writes: the checkouts of the project's
// repositories and where each one's last export is kept.
type exporter struct {
	git   ports.DesignExportGit
	repos ports.DesignSourceRepositories
	store ports.DesignExportStore

	mu        sync.Mutex
	checkouts map[string]*sync.Mutex
}

// UseExport sets what an export reads and writes. Without it the export use
// cases answer EXPORT_UNAVAILABLE.
func (s *Service) UseExport(git ports.DesignExportGit, repos ports.DesignSourceRepositories, store ports.DesignExportStore) {
	s.export = &exporter{git: git, repos: repos, store: store, checkouts: map[string]*sync.Mutex{}}
}

func (s *Service) exportReady() error {
	if s.export == nil {
		return &domain.StructuredError{Code: "EXPORT_UNAVAILABLE", Message: "exporting a design system is not configured"}
	}
	return nil
}

func (e *exporter) lockCheckout(root string) func() {
	e.mu.Lock()
	l, ok := e.checkouts[root]
	if !ok {
		l = &sync.Mutex{}
		e.checkouts[root] = l
	}
	e.mu.Unlock()
	l.Lock()
	return l.Unlock
}

// managed says whether a path relative to the export folder is one the export
// writes: DESIGN.md, tokens.json and the component pages.
func managed(rel string) bool {
	return rel == "DESIGN.md" || rel == "tokens.json" || (strings.HasPrefix(rel, "components/") && strings.HasSuffix(rel, ".html") && path.Dir(rel) == "components")
}

// wanted are the files an export of the design system's current version
// writes, relative to the export folder: the generator's files minus README.md.
// A version with an empty record still has DESIGN.md and tokens.json; a design
// system with no version has nothing to write (DESIGN_SYSTEM_EMPTY).
func (s *Service) wanted(ctx context.Context, designSystemID string) (map[string][]byte, *domain.DesignSystemVersion, error) {
	id, cur, err := s.current(ctx, designSystemID)
	if err != nil {
		return nil, nil, err
	}
	if cur == nil {
		return nil, nil, emptyExport()
	}
	files := map[string][]byte{}
	ident := Identity{Name: id.Name, Slug: id.Slug, Description: id.Description}
	for _, f := range Files(ident, cur.Version, cur.At, cur.Record) {
		if f.Path != "README.md" {
			files[f.Path] = f.Content
		}
	}
	if len(files) == 0 {
		day := cur.At.UTC().Format("2006-01-02")
		files["DESIGN.md"] = []byte(designMD(ident, cur.Version, day, cur.Record))
		files["tokens.json"] = tokensJSON(cur.Version, day, cur.Record)
	}
	return files, cur, nil
}

func emptyExport() error {
	return &domain.StructuredError{Code: "DESIGN_SYSTEM_EMPTY", Message: "the design system has no version yet; save one before exporting"}
}

// digest is the hash of a set of files: their paths and bytes.
func digest(files map[string][]byte) string {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, p := range paths {
		sum := sha256.Sum256(files[p])
		fmt.Fprintf(h, "%s\x00%x\n", p, sum)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// differing are the paths whose bytes differ between a and b, or that only
// one of them has, sorted.
func differing(a, b map[string][]byte) []string {
	var out []string
	for p, x := range a {
		if y, ok := b[p]; !ok || !bytes.Equal(x, y) {
			out = append(out, p)
		}
	}
	for p := range b {
		if _, ok := a[p]; !ok {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// classify is where files at the export folder stand against what the current
// version writes and what the last export wrote.
func classify(rec *ports.DesignExportRecord, designSystemID string, want, have map[string][]byte) (string, []string) {
	switch {
	case len(have) > 0 && digest(have) == digest(want):
		return ports.DesignExportInSync, nil
	case len(have) == 0 && rec == nil:
		return ports.DesignExportNeverExported, nil
	case rec != nil && rec.DesignSystemID == designSystemID && len(have) > 0 && digest(have) == rec.SHA:
		return ports.DesignExportBehind, nil
	}
	return ports.DesignExportEditedOutside, differing(want, have)
}

// applied is the repository and the design system it applies, NO_DESIGN_SYSTEM
// when it applies none.
func (s *Service) applied(ctx context.Context, repositoryID string) (*domain.Repository, error) {
	repo, err := s.export.repos.GetRepository(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	if repo.DesignSystemID == nil {
		return nil, &domain.StructuredError{Code: "NO_DESIGN_SYSTEM", Message: "the repository uses no design system; apply one first"}
	}
	return repo, nil
}

// managedFiles keeps the files of a folder the export owns.
func managedFiles(tree map[string][]byte) map[string][]byte {
	out := map[string][]byte{}
	for p, b := range tree {
		if managed(p) {
			out[p] = b
		}
	}
	return out
}

// ListDesignSystemExports implements ports.DesignSystemExports. Each
// repository costs a few git runs, read in parallel.
func (s *Service) ListDesignSystemExports(ctx context.Context, designSystemID string) (map[string]*ports.DesignExportInfo, error) {
	if err := s.exportReady(); err != nil {
		return nil, err
	}
	id, cur, err := s.current(ctx, designSystemID)
	if err != nil {
		return nil, err
	}
	repos, err := s.export.repos.ListRepositories(ctx, id.ProjectID)
	if err != nil {
		return nil, err
	}
	var want map[string][]byte
	version := 0
	if cur != nil {
		version = cur.Version
		if w, _, err := s.wanted(ctx, designSystemID); err == nil {
			want = w
		}
	}
	out := map[string]*ports.DesignExportInfo{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, r := range repos {
		if r.DesignSystemID == nil || *r.DesignSystemID != designSystemID {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			info := s.exportInfo(ctx, r, designSystemID, version, want)
			mu.Lock()
			out[r.ID] = info
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out, nil
}

// exportInfo is one repository's state. A checkout that cannot be read does
// not fail the list: the state then follows what the last export recorded.
func (s *Service) exportInfo(ctx context.Context, repo *domain.Repository, designSystemID string, version int, want map[string][]byte) *ports.DesignExportInfo {
	info := &ports.DesignExportInfo{State: ports.DesignExportNeverExported, CurrentVersion: version}
	rec, err := s.export.store.Get(ctx, repo.ID)
	if err != nil {
		return info
	}
	if rec != nil {
		at := rec.At
		info.ExportedVersion, info.ExportedCommit, info.ExportedAt = &rec.Version, rec.Commit, &at
	}
	recorded := func() {
		switch {
		case rec == nil:
			info.State = ports.DesignExportNeverExported
		case rec.DesignSystemID != designSystemID:
			info.State = ports.DesignExportEditedOutside
		case rec.Version == version:
			info.State = ports.DesignExportInSync
		default:
			info.State = ports.DesignExportBehind
		}
	}
	have, err := s.defaultBranchFolder(ctx, repo)
	if err != nil || want == nil {
		recorded()
		return info
	}
	info.State, info.ChangedFiles = classify(rec, designSystemID, want, have)
	return info
}

// defaultBranchFolder reads the export folder as the default branch has it.
func (s *Service) defaultBranchFolder(ctx context.Context, repo *domain.Repository) (map[string][]byte, error) {
	branch, err := s.export.git.DefaultBranch(ctx, repo.RootDir)
	if err != nil || branch == "" {
		return nil, fmt.Errorf("the checkout at %s has no default branch: %v", repo.RootDir, err)
	}
	tree, err := s.export.git.ReadTree(ctx, repo.RootDir, branch, repo.DesignSystemPath)
	if err != nil {
		return nil, err
	}
	return managedFiles(tree), nil
}

// PreviewDesignSystemExport implements ports.DesignSystemExports.
func (s *Service) PreviewDesignSystemExport(ctx context.Context, repositoryID string) ([]ports.DesignExportFile, error) {
	if err := s.exportReady(); err != nil {
		return nil, err
	}
	repo, err := s.applied(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	want, _, err := s.wanted(ctx, *repo.DesignSystemID)
	if err != nil {
		return nil, err
	}
	have, err := s.defaultBranchFolder(ctx, repo)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(want)+len(have))
	for p := range want {
		paths = append(paths, p)
	}
	for p := range have {
		if _, ok := want[p]; !ok {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	files := make([]ports.DesignExportFile, 0, len(paths))
	for _, p := range paths {
		f := ports.DesignExportFile{Path: repo.DesignSystemPath + p}
		w, inWant := want[p]
		h, inHave := have[p]
		switch {
		case !inHave:
			f.Status = "added"
		case !inWant:
			f.Status = "deleted"
		case bytes.Equal(w, h):
			f.Status = "unchanged"
		default:
			f.Status = "changed"
		}
		if f.Status != "unchanged" {
			f.Diff, err = difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
				A: difflib.SplitLines(string(h)), B: difflib.SplitLines(string(w)),
				FromFile: "a/" + f.Path, ToFile: "b/" + f.Path, Context: 3,
			})
			if err != nil {
				return nil, err
			}
		}
		files = append(files, f)
	}
	return files, nil
}

// ExportDesignSystem implements ports.DesignSystemExports.
func (s *Service) ExportDesignSystem(ctx context.Context, repositoryID string, confirmEditedOutside bool) (*ports.DesignExportResult, error) {
	if err := s.exportReady(); err != nil {
		return nil, err
	}
	repo, err := s.applied(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	designSystemID := *repo.DesignSystemID
	defer s.export.lockCheckout(repo.RootDir)()

	branch, err := s.cleanDefaultBranch(ctx, repo)
	if err != nil {
		return nil, err
	}
	want, cur, err := s.wanted(ctx, designSystemID)
	if err != nil {
		return nil, err
	}
	tree, err := s.export.git.ReadTree(ctx, repo.RootDir, branch, repo.DesignSystemPath)
	if err != nil {
		return nil, err
	}
	have := managedFiles(tree)
	rec, err := s.export.store.Get(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	state, changed := classify(rec, designSystemID, want, have)
	if state == ports.DesignExportEditedOutside && !confirmEditedOutside {
		return nil, &domain.DetailedError{
			StructuredError: &domain.StructuredError{Code: "EDITED_OUTSIDE", Message: "files at " + repo.DesignSystemPath + " were edited outside the harness; exporting replaces them"},
			Details:         map[string]any{"changed_files": changed},
		}
	}

	var commit string
	at := s.now()
	if state == ports.DesignExportInSync {
		if rec != nil && rec.DesignSystemID == designSystemID && rec.Version == cur.Version {
			commit, at = rec.Commit, rec.At
		} else if commit, err = s.export.git.LastCommit(ctx, repo.RootDir, branch, repo.DesignSystemPath); err != nil {
			return nil, err
		}
	} else {
		write := make(map[string][]byte, len(want))
		for p, b := range want {
			write[repo.DesignSystemPath+p] = b
		}
		var removed []string
		for p := range have {
			if _, ok := want[p]; !ok {
				removed = append(removed, repo.DesignSystemPath+p)
			}
		}
		slices.Sort(removed)
		message := fmt.Sprintf("design system: export version %d", cur.Version)
		if commit, err = s.export.git.CommitChanges(ctx, repo.RootDir, write, removed, message); err != nil {
			return nil, err
		}
	}
	if err := s.export.store.Set(ctx, &ports.DesignExportRecord{
		RepositoryID: repositoryID, DesignSystemID: designSystemID, Version: cur.Version,
		Commit: commit, At: at, SHA: digest(want),
	}); err != nil {
		return nil, err
	}
	s.announceExport(repo.ProjectID, designSystemID, repositoryID, ports.DesignExportInSync)
	return &ports.DesignExportResult{Commit: commit, Version: cur.Version, State: ports.DesignExportInSync}, nil
}

func (s *Service) announceExport(projectID, designSystemID, repositoryID, state string) {
	if s.feed == nil {
		return
	}
	s.feed.AnnounceChange(ports.ProjectChange{DesignSystemExport: &ports.DesignSystemExportChange{
		ProjectID: projectID, DesignSystemID: designSystemID, RepositoryID: repositoryID, State: state,
	}})
}

// cleanDefaultBranch is the repository's default branch, once the checkout is
// on it with nothing uncommitted: WRONG_BRANCH or CHECKOUT_DIRTY otherwise
// (the architecture export's rule).
func (s *Service) cleanDefaultBranch(ctx context.Context, repo *domain.Repository) (string, error) {
	branch, err := s.export.git.DefaultBranch(ctx, repo.RootDir)
	if err != nil {
		return "", err
	}
	state, err := s.export.git.Checkout(ctx, repo.RootDir)
	if err != nil {
		return "", err
	}
	switch {
	case branch == "" || state.Branch != branch:
		return "", &domain.StructuredError{Code: "WRONG_BRANCH", Message: "the checkout at " + repo.RootDir + " is on " + branchWord(state.Branch) + ", not on the default branch " + branch + "; check " + branch + " out there first"}
	case !state.Clean():
		return "", &domain.StructuredError{Code: "CHECKOUT_DIRTY", Message: "the checkout at " + repo.RootDir + " has uncommitted changes; commit, stash or remove them first"}
	}
	return branch, nil
}

func branchWord(b string) string {
	if b == "" {
		return "a detached HEAD"
	}
	return "branch " + b
}
