package designsystem

import (
	"context"
	"fmt"
	"path"
	"reflect"
	"strings"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.DesignSystemImports = (*Service)(nil)

const (
	defaultDesignMDPath  = "DESIGN.md"
	defaultTokensPath    = "tokens.json"
	defaultComponentsDir = "components/"
)

// UseImport sets what an import reads: a checkout's default branch and the
// project's repositories. Without it the import use cases answer
// IMPORT_UNAVAILABLE.
func (s *Service) UseImport(git ports.ArchitectureGit, repos ports.DesignSourceRepositories) {
	s.git, s.repos = git, repos
}

func (s *Service) importReady() error {
	if s.git == nil || s.repos == nil {
		return &domain.StructuredError{Code: "IMPORT_UNAVAILABLE", Message: "importing a design system is not configured"}
	}
	return nil
}

// FindDesignSystemSources implements ports.DesignSystemImports.
func (s *Service) FindDesignSystemSources(ctx context.Context, projectID string) ([]ports.DesignSource, error) {
	if err := s.importReady(); err != nil {
		return nil, err
	}
	if _, err := s.projects.GetProject(ctx, projectID); err != nil {
		return nil, err
	}
	repos, err := s.repos.ListRepositories(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := []ports.DesignSource{}
	for _, r := range repos {
		if r.RootDir == "" {
			continue
		}
		branch, err := s.git.DefaultBranch(ctx, r.RootDir)
		if err != nil || branch == "" {
			continue // a checkout that cannot be read holds nothing to import
		}
		files, err := s.git.Files(ctx, r.RootDir, branch)
		if err != nil {
			continue
		}
		src := ports.DesignSource{RepositoryID: r.ID, Name: r.DisplayName(), DesignMD: findFile(files, defaultDesignMDPath), Tokens: findFile(files, defaultTokensPath)}
		if src.DesignMD != "" || src.Tokens != "" {
			out = append(out, src)
		}
	}
	return out, nil
}

// findFile is the file called name at the root, else the first one level down
// (files are sorted), else "".
func findFile(files []string, name string) string {
	found := ""
	for _, f := range files {
		if f == name {
			return f
		}
		if found == "" && path.Base(f) == name && path.Dir(path.Dir(f)) == "." {
			found = f
		}
	}
	return found
}

// PreviewDesignSystemImport implements ports.DesignSystemImports.
func (s *Service) PreviewDesignSystemImport(ctx context.Context, req ports.DesignImportRequest) (*ports.DesignImportPreview, error) {
	p, _, err := s.read(ctx, req)
	return p, err
}

// read parses the repository's files into a preview and answers the
// repository too.
func (s *Service) read(ctx context.Context, req ports.DesignImportRequest) (*ports.DesignImportPreview, *domain.Repository, error) {
	if err := s.importReady(); err != nil {
		return nil, nil, err
	}
	if _, err := s.projects.GetProject(ctx, req.ProjectID); err != nil {
		return nil, nil, err
	}
	repo, err := s.repos.GetRepository(ctx, req.RepositoryID)
	if err != nil {
		return nil, nil, err
	}
	if repo.ProjectID != req.ProjectID {
		return nil, nil, &domain.StructuredError{Code: "CROSS_PROJECT_ACCESS", Message: "the repository belongs to another project"}
	}
	branch, err := s.git.DefaultBranch(ctx, repo.RootDir)
	if err != nil || branch == "" {
		return nil, nil, &domain.StructuredError{Code: "IMPORT_UNREADABLE", Message: "the repository's checkout has no default branch to read"}
	}
	designPath, tokensPath, dir := orDefault(req.DesignMDPath, defaultDesignMDPath), orDefault(req.TokensPath, defaultTokensPath), orDefault(req.ComponentsDir, defaultComponentsDir)

	pv := &ports.DesignImportPreview{Skipped: []ports.DesignImportSkip{}}
	var problems []string
	// A file that is not there is skipped; one that is there and does not
	// parse refuses the import, so nothing partial is stored.
	var unparsed []string
	note := func(file, why string) {
		pv.Skipped = append(pv.Skipped, skip{Path: file, Reason: why})
		problems = append(problems, fmt.Sprintf("%s: %s", file, why))
	}
	cannotParse := func(file string, err error) {
		note(file, "cannot parse: "+err.Error())
		unparsed = append(unparsed, file)
	}

	var designTokens, jsonTokens []domain.DesignToken
	if data, ok, err := s.git.ReadFile(ctx, repo.RootDir, branch, designPath); err != nil || !ok {
		note(designPath, notFound(branch, err))
	} else if _, tokens, principles, skipped, err := ParseDesignMD(data); err != nil {
		cannotParse(designPath, err)
	} else {
		pv.FoundDesignMD, designTokens, pv.Record.Principles = true, tokens, principles
		for _, k := range skipped {
			k.Path = qualified(designPath, k.Path)
			pv.Skipped = append(pv.Skipped, k)
		}
	}
	if data, ok, err := s.git.ReadFile(ctx, repo.RootDir, branch, tokensPath); err != nil || !ok {
		note(tokensPath, notFound(branch, err))
	} else if tokens, skipped, err := ParseTokensJSON(data); err != nil {
		cannotParse(tokensPath, err)
	} else {
		pv.FoundTokens, jsonTokens = true, tokens
		pv.Skipped = append(pv.Skipped, skipped...)
	}
	if len(unparsed) > 0 || (!pv.FoundDesignMD && !pv.FoundTokens) {
		return nil, nil, &domain.StructuredError{Code: "IMPORT_UNREADABLE", Message: "the design system files could not be read, so nothing is imported: " + strings.Join(problems, "; ")}
	}

	pv.Record.Tokens = mergeTokens(jsonTokens, designTokens, designPath, tokensPath, &pv.Skipped)
	s.readComponents(ctx, repo, branch, dir, pv)
	pv.Record.Normalize()
	pv.Counts = map[string]int{}
	for _, t := range pv.Record.Tokens {
		pv.Counts[t.Group]++
	}
	if problems := pv.Record.Validate(domain.DesignSystemRecord{}); len(problems) > 0 {
		return nil, nil, invalid(problems)
	}
	return pv, repo, nil
}

// qualified names an entry of a file in the skipped list.
func qualified(file, key string) string { return file + ": " + key }

func orDefault(v, def string) string {
	if v = strings.TrimSpace(v); v != "" {
		return v
	}
	return def
}

func notFound(branch string, err error) string {
	if err != nil {
		return "cannot be read: " + err.Error()
	}
	return "not found on branch " + branch
}

// mergeTokens is tokens.json's tokens, then those of DESIGN.md it lacks. For a
// token both have, tokens.json wins and a difference is listed as skipped from
// DESIGN.md.
func mergeTokens(fromJSON, fromDesign []domain.DesignToken, designPath, tokensPath string, skipped *[]skip) []domain.DesignToken {
	out := make([]domain.DesignToken, 0, len(fromJSON)+len(fromDesign))
	index := map[string]int{}
	for _, t := range fromJSON {
		if _, dup := index[t.Key()]; dup {
			*skipped = append(*skipped, skip{Path: qualified(tokensPath, t.Key()), Reason: "defined more than once; the first one stays"})
			continue
		}
		index[t.Key()] = len(out)
		out = append(out, t)
	}
	for _, t := range fromDesign {
		i, ok := index[t.Key()]
		switch {
		case !ok:
			index[t.Key()] = len(out)
			out = append(out, t)
		case !reflect.DeepEqual(out[i].Value, t.Value):
			*skipped = append(*skipped, skip{Path: qualified(designPath, t.Key()), Reason: fmt.Sprintf("differs from %s, which wins", tokensPath)})
		}
	}
	return out
}

// readComponents adds the components of dir's html files that parse and fit
// the tokens; the others are skipped.
func (s *Service) readComponents(ctx context.Context, repo *domain.Repository, branch, dir string, pv *ports.DesignImportPreview) {
	files, err := s.git.Files(ctx, repo.RootDir, branch)
	if err != nil {
		return
	}
	dir = strings.TrimSuffix(dir, "/") + "/"
	if dir == "./" {
		dir = ""
	}
	seen := map[string]bool{}
	for _, f := range files {
		rest, ok := strings.CutPrefix(f, dir)
		if !ok || strings.Contains(rest, "/") || !strings.HasSuffix(rest, ".html") {
			continue
		}
		data, found, err := s.git.ReadFile(ctx, repo.RootDir, branch, f)
		if err != nil || !found {
			pv.Skipped = append(pv.Skipped, skip{Path: f, Reason: notFound(branch, err)})
			continue
		}
		c, err := ParseComponentHTML(data)
		if err != nil {
			pv.Skipped = append(pv.Skipped, skip{Path: f, Reason: "cannot parse: " + err.Error()})
			continue
		}
		if seen[c.Slug] {
			pv.Skipped = append(pv.Skipped, skip{Path: f, Reason: fmt.Sprintf("component %q is defined more than once", c.Slug)})
			continue
		}
		trial := domain.DesignSystemRecord{Tokens: pv.Record.Tokens, Components: []domain.DesignComponent{c}}
		if problems := trial.Validate(domain.DesignSystemRecord{}); len(problems) > 0 {
			pv.Skipped = append(pv.Skipped, skip{Path: f, Reason: problems[0].Message})
			continue
		}
		seen[c.Slug] = true
		pv.Record.Components = append(pv.Record.Components, c)
		pv.FoundComponents++
	}
}

// ImportDesignSystem implements ports.DesignSystemImports.
func (s *Service) ImportDesignSystem(ctx context.Context, req ports.DesignImportRequest, name, slug, description string) (*domain.DesignSystemIdentity, *domain.DesignSystemVersion, error) {
	pv, repo, err := s.read(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	name = strings.TrimSpace(name)
	if err := invalidIdentity(append(domain.ValidateDesignSystemName(name), domain.ValidateDesignSystemSlug(slug)...)); err != nil {
		return nil, nil, err
	}
	id := &domain.DesignSystemIdentity{ProjectID: req.ProjectID, Name: name, Slug: slug, Description: strings.TrimSpace(description)}
	if err := s.store.Create(ctx, id); err != nil {
		return nil, nil, err
	}
	v := &domain.DesignSystemVersion{
		DesignSystemID: id.ID, At: s.now(), By: domain.DesignByImport, Source: domain.DesignSourceImport,
		RepositoryID: repo.ID, RepositoryName: repo.DisplayName(),
		Message: "Imported from " + repo.DisplayName(), Summary: domain.SummarizeDesignChange(domain.DesignSystemRecord{}, pv.Record), Record: pv.Record,
	}
	if err := s.store.Append(ctx, v, 0); err != nil {
		_ = s.store.Delete(ctx, id.ID) // all or nothing
		return nil, nil, err
	}
	s.announceChanged(id.ProjectID, id.ID)
	s.announceSaved(id.ProjectID, v)
	return id, v, nil
}
