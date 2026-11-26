package contextmap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.ContextMaps = (*Service)(nil)

// Settings is the effective configuration the map reads (the agent system
// settings store).
type Settings interface {
	Effective(ctx context.Context, projectID, repositoryID string) (domain.EffectiveAgentSystem, error)
}

// Architecture is what the map points to: the repository's architecture
// record and instruction files.
type Architecture interface {
	GetRepositoryArchitecture(ctx context.Context, repositoryID string) (*domain.RepositoryArchitecture, error)
	ListInstructionFiles(ctx context.Context, repositoryID string) ([]domain.InstructionFile, error)
}

// Deps are the Service's collaborators.
type Deps struct {
	Store        ports.ContextMapStore
	Repositories ports.RepositoryReader
	Git          ports.ArchitectureGit
	Settings     Settings
	Architecture Architecture
	Feed         ports.ProjectChangeSink
	Now          func() time.Time
}

// Service keeps each repository's context map.
type Service struct {
	d  Deps
	mu sync.Mutex // regenerations exclude each other
}

// New returns a Service.
func New(d Deps) *Service {
	if d.Now == nil {
		d.Now = time.Now
	}
	return &Service{d: d}
}

// UseFeed sets where a regeneration is announced.
func (s *Service) UseFeed(f ports.ProjectChangeSink) { s.d.Feed = f }

// GetContextMap implements ports.ContextMaps: the stored map when the default
// branch is still at its commit, else a map regenerated from the branch now.
func (s *Service) GetContextMap(ctx context.Context, repositoryID string) (*ports.ContextMapView, error) {
	repo, err := s.d.Repositories.GetRepository(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	view := &ports.ContextMapView{RepositoryID: repo.ID}
	eff, err := s.d.Settings.Effective(ctx, repo.ProjectID, repo.ID)
	if err != nil {
		return nil, err
	}
	if !eff.Learn.ContextMap.Enabled.Value {
		return view, nil
	}
	view.Enabled = true
	branch, err := s.d.Git.DefaultBranch(ctx, repo.RootDir)
	if err != nil || branch == "" {
		return view, nil
	}
	tip, err := s.d.Git.LastCommit(ctx, repo.RootDir, branch, "")
	if err != nil || tip == "" {
		return view, nil
	}
	in := s.inputs(ctx, repo, eff)
	inputs := fingerprint(in)
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, err := s.d.Store.GetContextMap(ctx, repo.ID)
	if err != nil {
		return nil, err
	}
	if stored == nil || stored.Commit != tip || stored.Inputs != inputs {
		m, err := s.regenerate(ctx, repo, in, inputs, branch, tip)
		if err != nil {
			return nil, err
		}
		stored = m
	}
	t := stored.GeneratedAt
	view.Available, view.Commit, view.Content, view.GeneratedAt = true, stored.Commit, stored.Content, &t
	return view, nil
}

// inputs is what a map is generated from besides the branch's files: the
// effective gate commands, the instruction files and the architecture layers.
func (s *Service) inputs(ctx context.Context, repo *domain.Repository, eff domain.EffectiveAgentSystem) Input {
	var in Input
	for _, g := range eff.Guard.Gates {
		if cmd := strings.TrimSpace(g.Command.Value); cmd != "" {
			in.Gates = append(in.Gates, Gate{Kind: string(g.Kind), Command: cmd})
		}
	}
	if s.d.Architecture != nil {
		if instr, err := s.d.Architecture.ListInstructionFiles(ctx, repo.ID); err == nil {
			for _, f := range instr {
				if f.Kind == "memory" {
					in.Instructions = append(in.Instructions, f.Path)
				}
			}
		}
		if arch, err := s.d.Architecture.GetRepositoryArchitecture(ctx, repo.ID); err == nil && arch != nil {
			for _, l := range arch.Record.Layers {
				if l.Match.Folder != "" {
					in.Layers = append(in.Layers, Layer{Name: l.Name, Folder: l.Match.Folder})
				}
			}
		}
	}
	return in
}

// fingerprint identifies the parts of in that do not come from the branch.
func fingerprint(in Input) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%q\n%q\n%q", in.Gates, in.Instructions, in.Layers)))
	return hex.EncodeToString(sum[:8])
}

func (s *Service) regenerate(ctx context.Context, repo *domain.Repository, in Input, inputs, branch, tip string) (*ports.StoredContextMap, error) {
	files, err := s.d.Git.Files(ctx, repo.RootDir, branch)
	if err != nil {
		return nil, err
	}
	in.Repository, in.Branch, in.Commit, in.Files = repo.Name, branch, tip, files
	in.Read = func(p string) ([]byte, bool) {
		b, ok, err := s.d.Git.ReadFile(ctx, repo.RootDir, branch, p)
		return b, ok && err == nil
	}
	m := ports.StoredContextMap{RepositoryID: repo.ID, Commit: tip, Inputs: inputs, Content: Generate(in), GeneratedAt: s.d.Now()}
	if err := s.d.Store.PutContextMap(ctx, m); err != nil {
		return nil, err
	}
	if s.d.Feed != nil {
		s.d.Feed.AnnounceChange(ports.ProjectChange{ContextMapRegenerated: &ports.ContextMapChange{RepositoryID: repo.ID, Commit: tip, ProjectID: repo.ProjectID}})
	}
	return &m, nil
}
