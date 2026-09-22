package configrepo

import (
	"github.com/rfbatista/harnesskit/mcpserver"
	"github.com/rfbatista/harnesskit/skill"

	"operators-mcp/internal/domain"
)

// The three interfaces below are declared locally, matching
// ports.AgentRepository / skill.Store / mcpserver.Store's method sets
// structurally, so this file doesn't need to import the ports package (which
// would otherwise import this one back through PersistenceModule's wiring).

type agentRepo interface {
	Get(id string) *domain.Agent
	List() []*domain.Agent
	Create(name, description, promptID string, skillIDs, mcpServerIDs []string) (*domain.Agent, error)
	Update(id, name, description, promptID string, skillIDs, mcpServerIDs []string) (*domain.Agent, error)
	Delete(id string) error
}

type skillRepo interface {
	Get(id string) *skill.Skill
	List() []*skill.Skill
	Create(input skill.Input) (*skill.Skill, error)
	Update(id string, input skill.Input) (*skill.Skill, error)
	Delete(id string) error
	ListFiles(skillID string) ([]skill.File, error)
	PutFile(skillID string, f skill.File) error
	RenameFile(skillID, oldPath, newPath string) error
	DeleteFile(skillID, path string) error
	SetPublishState(skillID string, state skill.PublishState) error
}

type mcpServerRepo interface {
	Get(id string) *mcpserver.Server
	List() []*mcpserver.Server
	Create(in mcpserver.Input) (*mcpserver.Server, error)
	Update(id string, in mcpserver.Input) (*mcpserver.Server, error)
	UpdateProbeResult(id string, result mcpserver.ProbeResult) (*mcpserver.Server, error)
	Delete(id string) error
}

// MergedAgentRepository combines a database-backed agent repository with a
// config-sourced one. List returns both, config-sourced winning on a name
// collision (agents.json is the live edit source). Get routes by id prefix.
// Writes always go to the database — a "cfg:" id sent to Update/Delete is
// refused rather than silently misrouted or acted on the wrong record.
type MergedAgentRepository struct {
	DB  agentRepo
	Cfg agentRepo
}

// MergeAgents composes a database-backed and a config-backed AgentRepository.
func MergeAgents(db, cfg agentRepo) *MergedAgentRepository {
	return &MergedAgentRepository{DB: db, Cfg: cfg}
}

func (m *MergedAgentRepository) List() []*domain.Agent {
	byName := map[string]*domain.Agent{}
	var order []string
	for _, a := range m.DB.List() {
		byName[a.Name] = a
		order = append(order, a.Name)
	}
	for _, a := range m.Cfg.List() {
		if _, exists := byName[a.Name]; !exists {
			order = append(order, a.Name)
		}
		byName[a.Name] = a
	}
	out := make([]*domain.Agent, 0, len(order))
	for _, name := range order {
		out = append(out, byName[name])
	}
	return out
}

func (m *MergedAgentRepository) Get(id string) *domain.Agent {
	if _, ok := stripIDPrefix(id); ok {
		return m.Cfg.Get(id)
	}
	return m.DB.Get(id)
}

func (m *MergedAgentRepository) Create(name, description, promptID string, skillIDs, mcpServerIDs []string) (*domain.Agent, error) {
	return m.DB.Create(name, description, promptID, skillIDs, mcpServerIDs)
}

func (m *MergedAgentRepository) Update(id, name, description, promptID string, skillIDs, mcpServerIDs []string) (*domain.Agent, error) {
	if _, ok := stripIDPrefix(id); ok {
		return nil, errReadOnly("agent")
	}
	return m.DB.Update(id, name, description, promptID, skillIDs, mcpServerIDs)
}

func (m *MergedAgentRepository) Delete(id string) error {
	if _, ok := stripIDPrefix(id); ok {
		return errReadOnly("agent")
	}
	return m.DB.Delete(id)
}

// MergedSkillStore is skill.Store's equivalent of MergedAgentRepository.
type MergedSkillStore struct {
	DB  skillRepo
	Cfg skillRepo
}

// MergeSkills composes a database-backed and a config-backed skill.Store.
func MergeSkills(db, cfg skillRepo) *MergedSkillStore {
	return &MergedSkillStore{DB: db, Cfg: cfg}
}

func (m *MergedSkillStore) List() []*skill.Skill {
	byName := map[string]*skill.Skill{}
	var order []string
	for _, s := range m.DB.List() {
		byName[s.Name] = s
		order = append(order, s.Name)
	}
	for _, s := range m.Cfg.List() {
		if _, exists := byName[s.Name]; !exists {
			order = append(order, s.Name)
		}
		byName[s.Name] = s
	}
	out := make([]*skill.Skill, 0, len(order))
	for _, name := range order {
		out = append(out, byName[name])
	}
	return out
}

func (m *MergedSkillStore) Get(id string) *skill.Skill {
	if _, ok := stripIDPrefix(id); ok {
		return m.Cfg.Get(id)
	}
	return m.DB.Get(id)
}

func (m *MergedSkillStore) Create(input skill.Input) (*skill.Skill, error) {
	return m.DB.Create(input)
}

func (m *MergedSkillStore) Update(id string, input skill.Input) (*skill.Skill, error) {
	if _, ok := stripIDPrefix(id); ok {
		return nil, errReadOnly("skill")
	}
	return m.DB.Update(id, input)
}

func (m *MergedSkillStore) Delete(id string) error {
	if _, ok := stripIDPrefix(id); ok {
		return errReadOnly("skill")
	}
	return m.DB.Delete(id)
}

// ListFiles routes by id prefix like Get, since it's a read.
func (m *MergedSkillStore) ListFiles(skillID string) ([]skill.File, error) {
	if _, ok := stripIDPrefix(skillID); ok {
		return m.Cfg.ListFiles(skillID)
	}
	return m.DB.ListFiles(skillID)
}

func (m *MergedSkillStore) PutFile(skillID string, f skill.File) error {
	if _, ok := stripIDPrefix(skillID); ok {
		return errReadOnly("skill")
	}
	return m.DB.PutFile(skillID, f)
}

func (m *MergedSkillStore) RenameFile(skillID, oldPath, newPath string) error {
	if _, ok := stripIDPrefix(skillID); ok {
		return errReadOnly("skill")
	}
	return m.DB.RenameFile(skillID, oldPath, newPath)
}

func (m *MergedSkillStore) DeleteFile(skillID, path string) error {
	if _, ok := stripIDPrefix(skillID); ok {
		return errReadOnly("skill")
	}
	return m.DB.DeleteFile(skillID, path)
}

func (m *MergedSkillStore) SetPublishState(skillID string, state skill.PublishState) error {
	if _, ok := stripIDPrefix(skillID); ok {
		return errReadOnly("skill")
	}
	return m.DB.SetPublishState(skillID, state)
}

// MergedMCPServerStore is mcpserver.Store's equivalent of MergedAgentRepository.
type MergedMCPServerStore struct {
	DB  mcpServerRepo
	Cfg mcpServerRepo
}

// MergeMCPServers composes a database-backed and a config-backed mcpserver.Store.
func MergeMCPServers(db, cfg mcpServerRepo) *MergedMCPServerStore {
	return &MergedMCPServerStore{DB: db, Cfg: cfg}
}

func (m *MergedMCPServerStore) List() []*mcpserver.Server {
	byName := map[string]*mcpserver.Server{}
	var order []string
	for _, s := range m.DB.List() {
		byName[s.Name] = s
		order = append(order, s.Name)
	}
	for _, s := range m.Cfg.List() {
		if _, exists := byName[s.Name]; !exists {
			order = append(order, s.Name)
		}
		byName[s.Name] = s
	}
	out := make([]*mcpserver.Server, 0, len(order))
	for _, name := range order {
		out = append(out, byName[name])
	}
	return out
}

func (m *MergedMCPServerStore) Get(id string) *mcpserver.Server {
	if _, ok := stripIDPrefix(id); ok {
		return m.Cfg.Get(id)
	}
	return m.DB.Get(id)
}

func (m *MergedMCPServerStore) Create(in mcpserver.Input) (*mcpserver.Server, error) {
	return m.DB.Create(in)
}

func (m *MergedMCPServerStore) Update(id string, in mcpserver.Input) (*mcpserver.Server, error) {
	if _, ok := stripIDPrefix(id); ok {
		return nil, errReadOnly("mcp server")
	}
	return m.DB.Update(id, in)
}

func (m *MergedMCPServerStore) UpdateProbeResult(id string, result mcpserver.ProbeResult) (*mcpserver.Server, error) {
	if _, ok := stripIDPrefix(id); ok {
		return nil, errReadOnly("mcp server")
	}
	return m.DB.UpdateProbeResult(id, result)
}

func (m *MergedMCPServerStore) Delete(id string) error {
	if _, ok := stripIDPrefix(id); ok {
		return errReadOnly("mcp server")
	}
	return m.DB.Delete(id)
}
