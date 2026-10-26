package repoarch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/xeipuuv/gojsonschema"
	"gopkg.in/yaml.v3"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

func sha(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// exportState tells whether the file in the checkout's default branch matches
// the record (ADR 0034). A checkout that cannot be read does not fail the
// page: the state then follows what the last export recorded.
func (s *Service) exportState(ctx context.Context, repo *domain.Repository, st *ports.StoredArchitecture) string {
	recorded := func() string {
		switch {
		case st.ExportedCommit == "":
			return domain.ExportNeverExported
		case st.ExportedVersion == st.Version:
			return domain.ExportInSync
		}
		return domain.ExportDatabaseAhead
	}
	if st.ExportedCommit == "" || s.git == nil {
		return domain.ExportNeverExported
	}
	branch, err := s.git.DefaultBranch(ctx, repo.RootDir)
	if err != nil || branch == "" {
		return recorded()
	}
	file, found, err := s.git.ReadFile(ctx, repo.RootDir, branch, domain.ArchitectureFilePath)
	if err != nil {
		return recorded()
	}
	if !found {
		return domain.ExportEditedOutside
	}
	current, err := ArchitectureYAML(st.Record)
	if err != nil {
		return recorded()
	}
	switch got := sha(file); {
	case got == sha(current):
		return domain.ExportInSync
	case got == st.ExportedSHA:
		return domain.ExportDatabaseAhead
	}
	if !followsSchema(file) {
		return domain.ExportFileInvalid
	}
	return domain.ExportEditedOutside
}

// followsSchema says whether text is YAML that satisfies the architecture
// schema.
func followsSchema(text []byte) bool {
	var doc any
	if yaml.Unmarshal(text, &doc) != nil {
		return false
	}
	raw, err := json.Marshal(normalizeYAML(doc))
	if err != nil {
		return false
	}
	res, err := gojsonschema.Validate(gojsonschema.NewBytesLoader(domain.ArchitectureSchemaJSON), gojsonschema.NewBytesLoader(raw))
	return err == nil && res.Valid()
}

// normalizeYAML makes yaml.v3's decoded value JSON-encodable: maps keyed by
// string.
func normalizeYAML(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, e := range t {
			t[k] = normalizeYAML(e)
		}
		return t
	case map[any]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			out[fmt.Sprint(k)] = normalizeYAML(e)
		}
		return out
	case []any:
		for i, e := range t {
			t[i] = normalizeYAML(e)
		}
		return t
	}
	return v
}

// ExportRepositoryArchitecture implements ports.RepositoryArchitectures.
func (s *Service) ExportRepositoryArchitecture(ctx context.Context, repositoryID string, version int) (*domain.ArchitectureExportResult, error) {
	repo, err := s.repos.GetRepository(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	if s.git == nil {
		return nil, &domain.StructuredError{Code: "UNAVAILABLE", Message: "the checkout cannot be written"}
	}
	defer s.lockCheckout(repo.RootDir)()

	st, err := s.store.Get(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, &domain.StructuredError{Code: "ARCHITECTURE_NOT_FOUND", Message: "there is no saved architecture to export"}
	}
	if st.Version != version {
		return nil, &domain.StructuredError{Code: "ARCHITECTURE_CHANGED", Message: fmt.Sprintf("version %d is not the current version, %d", version, st.Version)}
	}
	branch, err := s.git.DefaultBranch(ctx, repo.RootDir)
	if err != nil {
		return nil, err
	}
	state, err := s.git.Checkout(ctx, repo.RootDir)
	if err != nil {
		return nil, err
	}
	switch {
	case branch == "" || state.Branch != branch:
		return nil, &domain.StructuredError{Code: "WRONG_BRANCH", Message: "the checkout at " + repo.RootDir + " is on " + branchWord(state.Branch) + ", not on the default branch " + branch + "; check " + branch + " out there first"}
	case !state.Clean():
		return nil, &domain.StructuredError{Code: "CHECKOUT_DIRTY", Message: "the checkout at " + repo.RootDir + " has uncommitted changes; commit, stash or remove them first"}
	}

	text, err := ArchitectureYAML(st.Record)
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{domain.ArchitectureFilePath: text, domain.ArchitectureSchemaPath: domain.ArchitectureSchemaJSON}
	same := true
	for p, want := range files {
		have, found, err := s.git.ReadFile(ctx, repo.RootDir, branch, p)
		if err != nil {
			return nil, err
		}
		same = same && found && bytes.Equal(have, want)
	}
	var commit string
	if same {
		if commit, err = s.git.LastCommit(ctx, repo.RootDir, branch, domain.ArchitectureFilePath); err != nil {
			return nil, err
		}
	} else if commit, err = s.git.CommitFiles(ctx, repo.RootDir, files, fmt.Sprintf("chore(architecture): export repository architecture v%d", version)); err != nil {
		return nil, err
	}
	stored, err := s.store.SetExport(ctx, repositoryID, version, commit, time.Now(), sha(text))
	if err != nil {
		return nil, err
	}
	s.announce(repo.ProjectID, repositoryID, version)
	exp := s.exportOf(ctx, repo, stored)
	return &domain.ArchitectureExportResult{Commit: commit, Path: domain.ArchitectureFilePath, Export: exp}, nil
}

func branchWord(b string) string {
	if b == "" {
		return "a detached HEAD"
	}
	return "branch " + b
}
