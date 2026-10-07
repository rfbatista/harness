package sqlite

import (
	"fmt"
	"strings"

	"operators-mcp/internal/domain"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Open opens a SQLite database at the given path (e.g. "file:data.db" or ":memory:").
// It runs AutoMigrate for all models and performs data migrations.
func Open(path string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("sqlite open: %w", err)
	}
	if isMemoryDSN(path) {
		// Every new connection to an in-memory DSN opens its *own* empty
		// database, so as soon as the pool grows past one connection the
		// schema appears to vanish ("no such table: sessions"). Pinning the
		// pool keeps the whole process on one database.
		sqlDB, err := db.DB()
		if err != nil {
			return nil, fmt.Errorf("sqlite pool: %w", err)
		}
		sqlDB.SetMaxOpenConns(1)
	}
	if err := db.AutoMigrate(&ProjectModel{}, &RepositoryModel{}, &ZoneModel{}, &AgentModel{}, &PromptModel{}, &SkillModel{}, &SkillFileModel{}, &SettingModel{}, &MCPServerModel{}, &ToolModel{}, &TaskModel{}, &SessionModel{}, &SessionEventModel{}, &TicketModel{}, &DocumentModel{}, &TicketDocumentModel{}, &WorkspaceModel{}, &BoundedContextModel{}, &EnvFileModel{}, &RunCommandModel{}, &ArtifactModel{}, &ArtifactTicketModel{}, &TaskMessageModel{}, &ReviewRequestModel{}, &StatusCheckModel{}, &TicketStatusChangeModel{}); err != nil {
		return nil, fmt.Errorf("sqlite migrate: %w", err)
	}
	if err := migrateAgentPromptsToEntities(db); err != nil {
		return nil, fmt.Errorf("sqlite prompt migration: %w", err)
	}
	if err := migrateSkillsToFiles(db); err != nil {
		return nil, fmt.Errorf("sqlite skill files migration: %w", err)
	}
	if err := migrateInteractiveRunner(db); err != nil {
		return nil, fmt.Errorf("sqlite session runner migration: %w", err)
	}
	return db, nil
}

// migrateInteractiveRunner marks interactive sessions recorded before
// sessions said where they run as RunnerTUI: until then the client ran every
// one of them. Rows that already say are left alone, so it is safe to re-run.
func migrateInteractiveRunner(db *gorm.DB) error {
	return db.Model(&SessionModel{}).
		Where("interactive = ? AND (runs_on IS NULL OR runs_on = '')", true).
		Update("runs_on", "tui").Error
}

// isMemoryDSN reports whether the DSN names an in-memory database, in either
// spelling SQLite accepts (":memory:" or a file URI with mode=memory).
func isMemoryDSN(path string) bool {
	return strings.Contains(path, ":memory:") || strings.Contains(path, "mode=memory")
}

// migrateSkillsToFiles converts legacy dual-mode skills (inline "content" or an
// external "path") into the app-owned skill_files tree. It runs once per skill:
// skills that already have file rows are skipped, making it safe to re-run.
func migrateSkillsToFiles(db *gorm.DB) error {
	var skills []SkillModel
	if err := db.Find(&skills).Error; err != nil {
		return err
	}
	for i := range skills {
		s := &skills[i]
		var count int64
		if err := db.Model(&SkillFileModel{}).Where("skill_id = ?", s.ID).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			continue // already migrated
		}

		files := legacySkillFiles(s)
		if len(files) == 0 {
			continue
		}
		if err := db.Transaction(func(tx *gorm.DB) error {
			return replaceFiles(tx, s.ID, files)
		}); err != nil {
			return err
		}
	}
	return nil
}

// legacySkillFiles derives a stored file tree from a legacy skill record.
// Path skills import their on-disk tree (best-effort); inline skills become a
// single SKILL.md. As a last resort a stub SKILL.md is synthesized so the skill
// remains valid even if its source is gone.
func legacySkillFiles(s *SkillModel) []domain.SkillFile {
	if strings.TrimSpace(s.Path) != "" {
		if files, err := domain.ImportSkillTreeFromPath(s.Path); err == nil && len(files) > 0 {
			return files
		}
	}
	body := strings.TrimSpace(s.Content)
	if body == "" {
		if strings.TrimSpace(s.Name) == "" {
			return nil
		}
		body = domain.SkillMarkdownFor(s.Name, s.Description, "")
	} else if !strings.HasPrefix(body, "---") {
		body = domain.SkillMarkdownFor(s.Name, s.Description, body)
	}
	return []domain.SkillFile{{Path: domain.SkillFileName, Content: body}}
}

// migrateAgentPromptsToEntities migrates legacy agents that have a non-empty
// "prompt" text column into separate Prompt entities, setting PromptID on the
// agent. This is a one-time migration that is safe to run multiple times.
func migrateAgentPromptsToEntities(db *gorm.DB) error {
	if !db.Migrator().HasColumn(&AgentModel{}, "prompt") {
		return nil
	}

	type legacyAgent struct {
		ID     string
		Name   string
		Prompt string
	}
	var agents []legacyAgent
	if err := db.Raw("SELECT id, name, prompt FROM agents WHERE prompt != '' AND prompt IS NOT NULL AND (prompt_id IS NULL OR prompt_id = '')").Scan(&agents).Error; err != nil {
		return err
	}

	for _, a := range agents {
		id, err := genID()
		if err != nil {
			return err
		}
		prompt := PromptModel{ID: id, Name: a.Name + " prompt", Content: a.Prompt}
		if err := db.Create(&prompt).Error; err != nil {
			return err
		}
		if err := db.Exec("UPDATE agents SET prompt_id = ?, prompt = '' WHERE id = ?", id, a.ID).Error; err != nil {
			return err
		}
	}

	return nil
}
