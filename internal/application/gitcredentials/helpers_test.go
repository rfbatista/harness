package gitcredentials_test

import (
	"bytes"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/gitcredentials/gitcredentialstest"
	"operators-mcp/internal/ports"
)

func sqliteRepositoryCredentials(env *gitcredentialstest.SQLite) ports.RepositoryCredentialRepository {
	return sqlite.NewRepositoryCredentialRepository(env.DB)
}

// sqliteClose checkpoints and closes the database so its file holds every
// write.
func sqliteClose(env *gitcredentialstest.SQLite) error {
	if err := env.DB.Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error; err != nil {
		return err
	}
	db, err := env.DB.DB()
	if err != nil {
		return err
	}
	return db.Close()
}

func containsBytes(b []byte, s string) bool { return bytes.Contains(b, []byte(s)) }
