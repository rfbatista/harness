package sqlite

import (
	"database/sql"
	"errors"
	"fmt"
	"runtime"
	"strings"

	"operators-mcp/internal/domain"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// busyTimeoutMS is how long a connection waits for SQLite's lock before a
// call fails with "database is locked".
const busyTimeoutMS = 5000

// Open opens a SQLite database at the given path (e.g. "data.db",
// "file:data.db" or ":memory:"). It runs AutoMigrate for all models and
// performs data migrations.
//
// A file database runs in WAL (ADR 0005), switched on here when it was not.
// The returned handle writes through the writer connection, a pool of one
// whose transactions take the write lock when they begin, and sends reads
// made outside a transaction to the reader pool, whose connections refuse
// writes. Close releases both.
//
// Every database call slower than the slow-call threshold (WithSlowThreshold,
// DefaultSlowThreshold when unset) is logged as a slow database call.
func Open(path string, opts ...Option) (*gorm.DB, error) {
	slow := newSlowCalls(opts)
	if isMemoryDSN(path) {
		return openMemory(path, slow)
	}
	db, err := openWriter(fileDSN(path, "_journal_mode=WAL", "_synchronous=NORMAL", "_txlock=immediate"), slow)
	if err != nil {
		return nil, err
	}
	writer, _ := db.DB()
	if err := migrate(db); err != nil {
		writer.Close()
		return nil, err
	}
	// Registered after the migrations, so they run on the writer connection.
	if err := db.Use(&readerPool{dsn: fileDSN(path, "_query_only=true")}); err != nil {
		writer.Close()
		return nil, fmt.Errorf("sqlite reader pool: %w", err)
	}
	return db, nil
}

func openMemory(path string, slow *slowCalls) (*gorm.DB, error) {
	// Every new connection to an in-memory DSN opens its *own* empty
	// database, so as soon as the pool grows past one connection the
	// schema appears to vanish ("no such table: sessions"). openWriter's
	// pool of one keeps the whole process on one database.
	db, err := openWriter(path, slow)
	if err != nil {
		return nil, err
	}
	if err := migrate(db); err != nil {
		return nil, err
	}
	return db, nil
}

// openWriter opens dsn as the writer connection, a pool of one, with slow
// database calls logged.
func openWriter(dsn string, slow *slowCalls) (*gorm.DB, error) {
	pool, err := sql.Open(sqlite.DriverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlite open: %w", err)
	}
	pool.SetMaxOpenConns(1)
	db, err := gorm.Open(sqlite.New(sqlite.Config{Conn: &writerConn{DB: pool, slow: slow}}), &gorm.Config{Logger: slow.gormLogger()})
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("sqlite open: %w", err)
	}
	if err := measureBeginWait(db); err != nil {
		pool.Close()
		return nil, fmt.Errorf("sqlite slow-call logging: %w", err)
	}
	return db, nil
}

// fileDSN is path as a file URI with the busy timeout and params set on
// every connection it opens.
func fileDSN(path string, params ...string) string {
	if !strings.HasPrefix(path, "file:") {
		path = "file:" + path
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + strings.Join(append([]string{fmt.Sprintf("_busy_timeout=%d", busyTimeoutMS)}, params...), "&")
}

// Close closes db's writer connection and, for a file database, its reader
// pool.
func Close(db *gorm.DB) error {
	var errs []error
	if p := readerPoolOf(db); p != nil {
		errs = append(errs, p.pool.Close())
	}
	w, err := db.DB()
	if err == nil {
		err = w.Close()
	}
	return errors.Join(append(errs, err)...)
}

// Reader is db on its reader pool, which refuses writes. An in-memory
// database has no reader pool: Reader is db itself.
func Reader(db *gorm.DB) *gorm.DB {
	if p := readerPoolOf(db); p != nil {
		return p.db
	}
	return db
}

// readerPoolOf is db's reader pool; nil for an in-memory database.
func readerPoolOf(db *gorm.DB) *readerPool {
	p, _ := db.Config.Plugins[readerPoolName].(*readerPool)
	return p
}

const readerPoolName = "harness:reader-pool"

// readerPool routes reads made outside a transaction to its own connections,
// opened query_only. Everything else (writes, transactions and the reads in
// them, raw statements that are not a SELECT) stays on the writer connection.
type readerPool struct {
	dsn    string
	pool   *sql.DB
	db     *gorm.DB // on pool alone, for Reader
	writer gorm.ConnPool
}

func (p *readerPool) Name() string { return readerPoolName }

func (p *readerPool) Initialize(db *gorm.DB) error {
	pool, err := sql.Open(sqlite.DriverName, p.dsn)
	if err != nil {
		return err
	}
	// Measured with the page-load budget test (internal/app): once pages
	// read a constant number of queries, 10 parallel task pages finish as
	// fast with 4 readers as with 32, so the pool stays at max(4, NumCPU).
	pool.SetMaxOpenConns(max(4, runtime.NumCPU()))
	reader, err := gorm.Open(sqlite.New(sqlite.Config{Conn: pool}), &gorm.Config{Logger: db.Logger})
	if err != nil {
		pool.Close()
		return err
	}
	p.pool, p.db, p.writer = pool, reader, db.ConnPool
	if err := db.Callback().Query().Before("gorm:query").Register(readerPoolName, p.route); err != nil {
		return err
	}
	return db.Callback().Row().Before("gorm:row").Register(readerPoolName, p.route)
}

func (p *readerPool) route(db *gorm.DB) {
	if db.Statement.ConnPool != p.writer || !isRead(db.Statement.SQL.String()) {
		return // inside a transaction, or a statement that may write
	}
	db.Statement.ConnPool = p.pool
}

// isRead reports whether sql, as built so far, only reads: a statement GORM
// has yet to build (a query) or a raw SELECT.
func isRead(sql string) bool {
	sql = strings.TrimSpace(sql)
	return sql == "" || len(sql) >= 6 && strings.EqualFold(sql[:6], "SELECT")
}

func migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(&ProjectModel{}, &RepositoryModel{}, &ZoneModel{}, &AgentModel{}, &PromptModel{}, &SkillModel{}, &SkillFileModel{}, &SettingModel{}, &MCPServerModel{}, &ToolModel{}, &TaskModel{}, &SessionModel{}, &SessionEventModel{}, &SandboxViolationModel{}, &TicketModel{}, &DocumentModel{}, &TicketDocumentModel{}, &WorkspaceModel{}, &BoundedContextModel{}, &EnvFileModel{}, &RunCommandModel{}, &ArtifactModel{}, &ArtifactTicketModel{}, &TaskMessageModel{}, &ReviewRequestModel{}, &StatusCheckModel{}, &TicketStatusChangeModel{}, &TicketResourceModel{}, &TicketCommentModel{}, &PullRequestModel{}, &NotificationModel{}, &GitCredentialModel{}, &GitIdentityModel{}, &RepositoryCredentialModel{}, &UsageRecordModel{}, &ActivationModel{}, &TokenSaverInstallationModel{}); err != nil {
		return fmt.Errorf("sqlite migrate: %w", err)
	}
	if err := migrateAgentPromptsToEntities(db); err != nil {
		return fmt.Errorf("sqlite prompt migration: %w", err)
	}
	if err := migrateSkillsToFiles(db); err != nil {
		return fmt.Errorf("sqlite skill files migration: %w", err)
	}
	if err := migrateInteractiveRunner(db); err != nil {
		return fmt.Errorf("sqlite session runner migration: %w", err)
	}
	return nil
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
