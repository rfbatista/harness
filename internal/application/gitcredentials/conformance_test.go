package gitcredentials_test

import (
	"testing"

	"operators-mcp/internal/application/gitcredentials/gitcredentialstest"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

func TestGitCredentialsConformanceOverSQLite(t *testing.T) {
	portstest.GitCredentialsConformance(t, func(t *testing.T) (ports.GitCredentials, portstest.GitCredentialsFixture) {
		env := gitcredentialstest.NewSQLite(t)
		return env.Service, env.Fixture()
	})
}

func TestGitCredentialsConformanceOfTheFake(t *testing.T) {
	portstest.GitCredentialsConformance(t, func(t *testing.T) (ports.GitCredentials, portstest.GitCredentialsFixture) {
		f := gitcredentialstest.New()
		return f, portstest.GitCredentialsFixture{
			AddRepository: func(_ *testing.T, projectID, name, origin string) (string, string) {
				return f.AddRepository(projectID, name, origin)
			},
			Lock: func(*testing.T) { f.Lock() },
		}
	})
}
