package sqlite

import (
	"context"

	"operators-mcp/internal/domain"

	"gorm.io/gorm"
)

// AgentUsageRepository counts where each catalog agent is used, for the
// agent catalog's read.
type AgentUsageRepository struct {
	db *gorm.DB
}

// NewAgentUsageRepository returns a new agent usage repository.
func NewAgentUsageRepository(db *gorm.DB) *AgentUsageRepository {
	return &AgentUsageRepository{db: db}
}

// AgentUsage answers, by agent name, how many sessions ever ran as the agent
// (any project, any status) and how many architecture layer zones assign it.
// It takes one grouped query for each count, whatever the number of agents.
func (r *AgentUsageRepository) AgentUsage(ctx context.Context) (map[string]domain.AgentUsage, error) {
	out := map[string]domain.AgentUsage{}
	var sessions []struct {
		Name string
		N    int
	}
	if err := r.db.WithContext(ctx).Raw(
		`SELECT agent_id AS name, COUNT(*) AS n FROM sessions WHERE agent_id <> '' GROUP BY agent_id`,
	).Scan(&sessions).Error; err != nil {
		return nil, err
	}
	for _, s := range sessions {
		u := out[s.Name]
		u.Sessions = s.N
		out[s.Name] = u
	}
	var zones []struct {
		Name string
		N    int
	}
	if err := r.db.WithContext(ctx).Raw(
		`SELECT j.key AS name, COUNT(*) AS n FROM zones z, json_each(z.access_json, '$.agents') j
		 WHERE z.repository_id <> '' AND z.access_json <> '' GROUP BY j.key`,
	).Scan(&zones).Error; err != nil {
		return nil, err
	}
	for _, z := range zones {
		u := out[z.Name]
		u.Zones = z.N
		out[z.Name] = u
	}
	return out, nil
}
