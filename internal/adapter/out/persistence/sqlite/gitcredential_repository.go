package sqlite

import (
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// GitCredentialModel is a stored git credential. The secret is in Sealed,
// encrypted with the key KeyID names; nothing else here is secret.
type GitCredentialModel struct {
	ID          string `gorm:"primaryKey"`
	Name        string `gorm:"column:name"`
	Kind        string `gorm:"column:kind"`
	Host        string `gorm:"column:host;index"`
	Username    string `gorm:"column:username"`
	Fingerprint string `gorm:"column:fingerprint"`
	PublicKey   string `gorm:"column:public_key"`
	IsDefault   bool   `gorm:"column:is_default"`
	Sealed      []byte `gorm:"column:sealed"`
	KeyID       string `gorm:"column:key_id"`
	CreatedAt   int64  `gorm:"column:created_at"`
	UpdatedAt   int64  `gorm:"column:updated_at"`
	LastUsedAt  int64  `gorm:"column:last_used_at"`
}

func (GitCredentialModel) TableName() string { return "git_credentials" }

func gitSeconds(t time.Time) int64 { return t.Unix() }

func gitTimeOf(s int64) time.Time { return time.Unix(s, 0).UTC() }

func (m *GitCredentialModel) toPort() *ports.StoredGitCredential {
	c := domain.GitCredential{
		ID: m.ID, Name: m.Name, Kind: domain.GitCredentialKind(m.Kind), Host: m.Host, Username: m.Username,
		Fingerprint: m.Fingerprint, IsDefault: m.IsDefault,
		CreatedAt: gitTimeOf(m.CreatedAt), UpdatedAt: gitTimeOf(m.UpdatedAt),
	}
	if m.PublicKey != "" {
		pk := m.PublicKey
		c.PublicKey = &pk
	}
	if m.LastUsedAt != 0 {
		at := gitTimeOf(m.LastUsedAt)
		c.LastUsedAt = &at
	}
	return &ports.StoredGitCredential{Credential: c, Sealed: m.Sealed, KeyID: m.KeyID}
}

func gitCredentialModelOf(s *ports.StoredGitCredential) *GitCredentialModel {
	c := s.Credential
	m := &GitCredentialModel{
		ID: c.ID, Name: c.Name, Kind: string(c.Kind), Host: c.Host, Username: c.Username,
		Fingerprint: c.Fingerprint, IsDefault: c.IsDefault, Sealed: s.Sealed, KeyID: s.KeyID,
		CreatedAt: gitSeconds(c.CreatedAt), UpdatedAt: gitSeconds(c.UpdatedAt),
	}
	if c.PublicKey != nil {
		m.PublicKey = *c.PublicKey
	}
	if c.LastUsedAt != nil {
		m.LastUsedAt = gitSeconds(*c.LastUsedAt)
	}
	return m
}

// GitCredentialRepository implements ports.GitCredentialRepository.
type GitCredentialRepository struct{ db *gorm.DB }

var _ ports.GitCredentialRepository = (*GitCredentialRepository)(nil)

func NewGitCredentialRepository(db *gorm.DB) *GitCredentialRepository {
	return &GitCredentialRepository{db: db}
}

func (r *GitCredentialRepository) List() ([]*ports.StoredGitCredential, error) {
	var ms []GitCredentialModel
	if err := r.db.Order("id").Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]*ports.StoredGitCredential, 0, len(ms))
	for i := range ms {
		out = append(out, ms[i].toPort())
	}
	return out, nil
}

func (r *GitCredentialRepository) Get(id string) (*ports.StoredGitCredential, error) {
	var ms []GitCredentialModel
	if err := r.db.Where("id = ?", id).Limit(1).Find(&ms).Error; err != nil {
		return nil, err
	}
	if len(ms) == 0 {
		return nil, nil
	}
	return ms[0].toPort(), nil
}

func (r *GitCredentialRepository) Create(c *ports.StoredGitCredential) (*ports.StoredGitCredential, error) {
	id, err := genID()
	if err != nil {
		return nil, err
	}
	m := gitCredentialModelOf(c)
	m.ID = id
	if err := r.db.Create(m).Error; err != nil {
		return nil, err
	}
	return m.toPort(), nil
}

func (r *GitCredentialRepository) Update(c *ports.StoredGitCredential) error {
	res := r.db.Select("*").Where("id = ?", c.Credential.ID).Updates(gitCredentialModelOf(c))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New("git credential " + c.Credential.ID + " not found")
	}
	return nil
}

func (r *GitCredentialRepository) Delete(id string) error {
	return r.db.Where("id = ?", id).Delete(&GitCredentialModel{}).Error
}

// GitIdentityModel is the one commit identity; its id is always 1.
type GitIdentityModel struct {
	ID    int    `gorm:"primaryKey"`
	Name  string `gorm:"column:name"`
	Email string `gorm:"column:email"`
}

func (GitIdentityModel) TableName() string { return "git_identity" }

// GitIdentityRepository implements ports.GitIdentityRepository.
type GitIdentityRepository struct{ db *gorm.DB }

var _ ports.GitIdentityRepository = (*GitIdentityRepository)(nil)

func NewGitIdentityRepository(db *gorm.DB) *GitIdentityRepository {
	return &GitIdentityRepository{db: db}
}

func (r *GitIdentityRepository) Get() (*domain.GitIdentity, error) {
	var ms []GitIdentityModel
	if err := r.db.Where("id = 1").Limit(1).Find(&ms).Error; err != nil {
		return nil, err
	}
	if len(ms) == 0 {
		return nil, nil
	}
	return &domain.GitIdentity{Name: ms[0].Name, Email: ms[0].Email}, nil
}

func (r *GitIdentityRepository) Put(id domain.GitIdentity) error {
	return r.db.Clauses(clause.OnConflict{UpdateAll: true}).
		Create(&GitIdentityModel{ID: 1, Name: id.Name, Email: id.Email}).Error
}

// RepositoryCredentialModel pins a repository to a credential.
type RepositoryCredentialModel struct {
	RepositoryID string `gorm:"primaryKey;column:repository_id"`
	ProjectID    string `gorm:"column:project_id;index"`
	CredentialID string `gorm:"column:credential_id;index"`
}

func (RepositoryCredentialModel) TableName() string { return "git_repository_credentials" }

// RepositoryCredentialRepository implements ports.RepositoryCredentialRepository.
type RepositoryCredentialRepository struct{ db *gorm.DB }

var _ ports.RepositoryCredentialRepository = (*RepositoryCredentialRepository)(nil)

func NewRepositoryCredentialRepository(db *gorm.DB) *RepositoryCredentialRepository {
	return &RepositoryCredentialRepository{db: db}
}

func (r *RepositoryCredentialRepository) Get(repositoryID string) (string, error) {
	var ms []RepositoryCredentialModel
	if err := r.db.Where("repository_id = ?", repositoryID).Limit(1).Find(&ms).Error; err != nil {
		return "", err
	}
	if len(ms) == 0 {
		return "", nil
	}
	return ms[0].CredentialID, nil
}

func (r *RepositoryCredentialRepository) ListByRepositories(repositoryIDs []string) ([]ports.RepositoryCredentialPin, error) {
	if len(repositoryIDs) == 0 {
		return nil, nil
	}
	var ms []RepositoryCredentialModel
	if err := r.db.Where("repository_id IN ?", repositoryIDs).Order("repository_id").Find(&ms).Error; err != nil {
		return nil, err
	}
	return repositoryPins(ms), nil
}

func (r *RepositoryCredentialRepository) Put(pin ports.RepositoryCredentialPin) error {
	return r.db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&RepositoryCredentialModel{
		RepositoryID: pin.RepositoryID, ProjectID: pin.ProjectID, CredentialID: pin.CredentialID,
	}).Error
}

func (r *RepositoryCredentialRepository) Delete(repositoryID string) error {
	return r.db.Where("repository_id = ?", repositoryID).Delete(&RepositoryCredentialModel{}).Error
}

func (r *RepositoryCredentialRepository) ListByCredential(credentialID string) ([]ports.RepositoryCredentialPin, error) {
	var ms []RepositoryCredentialModel
	if err := r.db.Where("credential_id = ?", credentialID).Order("repository_id").Find(&ms).Error; err != nil {
		return nil, err
	}
	return repositoryPins(ms), nil
}

func repositoryPins(ms []RepositoryCredentialModel) []ports.RepositoryCredentialPin {
	out := make([]ports.RepositoryCredentialPin, 0, len(ms))
	for _, m := range ms {
		out = append(out, ports.RepositoryCredentialPin{RepositoryID: m.RepositoryID, ProjectID: m.ProjectID, CredentialID: m.CredentialID})
	}
	return out
}

func (r *RepositoryCredentialRepository) DeleteByCredential(credentialID string) error {
	return r.db.Where("credential_id = ?", credentialID).Delete(&RepositoryCredentialModel{}).Error
}

func (r *RepositoryCredentialRepository) DeleteByProject(projectID string) error {
	return r.db.Where("project_id = ?", projectID).Delete(&RepositoryCredentialModel{}).Error
}
