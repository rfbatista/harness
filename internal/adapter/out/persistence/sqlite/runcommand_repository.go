package sqlite

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.RunCommandRepository = (*RunCommandRepository)(nil)

// RunCommandRepository stores repositories' saved run commands.
type RunCommandRepository struct{ db *gorm.DB }

// NewRunCommandRepository returns the store.
func NewRunCommandRepository(db *gorm.DB) *RunCommandRepository { return &RunCommandRepository{db: db} }

// List returns a repository's commands by name.
func (r *RunCommandRepository) List(repositoryID string) []*domain.RunCommand {
	var models []RunCommandModel
	r.db.Where("repository_id = ?", repositoryID).Order("name").Find(&models)
	out := make([]*domain.RunCommand, 0, len(models))
	for _, m := range models {
		out = append(out, &domain.RunCommand{RepositoryID: m.RepositoryID, Name: m.Name, Command: m.Command, UpdatedAt: m.UpdatedAt})
	}
	return out
}

// Put creates or replaces the command with c's repository and name.
func (r *RunCommandRepository) Put(c *domain.RunCommand) (*domain.RunCommand, error) {
	m := RunCommandModel{RepositoryID: c.RepositoryID, Name: c.Name, Command: c.Command, UpdatedAt: time.Now()}
	err := r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "repository_id"}, {Name: "name"}},
		DoUpdates: clause.AssignmentColumns([]string{"command", "updated_at"}),
	}).Create(&m).Error
	if err != nil {
		return nil, err
	}
	return &domain.RunCommand{RepositoryID: m.RepositoryID, Name: m.Name, Command: m.Command, UpdatedAt: m.UpdatedAt}, nil
}

// Delete removes one command, or RUN_COMMAND_NOT_FOUND.
func (r *RunCommandRepository) Delete(repositoryID, name string) error {
	res := r.db.Where("repository_id = ? AND name = ?", repositoryID, name).Delete(&RunCommandModel{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return &domain.StructuredError{Code: "RUN_COMMAND_NOT_FOUND", Message: "run command " + name + " not found"}
	}
	return nil
}

// DeleteByRepository removes every command of a repository.
func (r *RunCommandRepository) DeleteByRepository(repositoryID string) error {
	return r.db.Where("repository_id = ?", repositoryID).Delete(&RunCommandModel{}).Error
}
