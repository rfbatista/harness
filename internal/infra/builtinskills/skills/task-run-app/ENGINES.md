# Engine detection

Read in this order. Stop at the first source that names exactly one engine. Name every
file you read in the Seed document.

| Source | What to look for | Says |
|---|---|---|
| `.env.example` | a driver key such as `DB_DRIVER`, `DATABASE_DRIVER` | its value: `postgres`, `mysql`, `sqlite` |
| `.env.example` | a `DATABASE_URL` or `DB_URL` scheme | `postgres://`, `postgresql://` → postgres; `mysql://` → mysql; `sqlite:`, `file:` → SQLite |
| `.env.example` | a `DB_PATH`, `SQLITE_PATH`, `*.db` default | SQLite |
| `.env.example` | `DB_PORT` default 5432 / 3306, `PG*`, `MYSQL_*` keys | postgres / mysql |
| Dependency manifests (`go.mod`, `package.json`, `requirements*.txt`, `pyproject.toml`, `Gemfile`, `pom.xml`) | `pgx`, `lib/pq`, `pg`, `psycopg`, `gorm.io/driver/postgres` | postgres |
| | `go-sql-driver/mysql`, `mysql2`, `PyMySQL`, `gorm.io/driver/mysql` | mysql |
| | `mattn/go-sqlite3`, `modernc.org/sqlite`, `better-sqlite3`, `sqlite3` | SQLite |
| Migration files | SQL dialect: `SERIAL`, `RETURNING`, `jsonb` → postgres; `AUTO_INCREMENT`, backticks → mysql; `AUTOINCREMENT`, `PRAGMA` → SQLite | the dialect |
| Compose or deploy files | an image `postgres`, `mysql`, `mariadb` | that engine |

Several drivers found and `.env.example` picks none: ask. A MariaDB image counts as mysql.
Any other engine (mongo, redis as the only store, …): say so and ask how to proceed.

## Finding a running engine in k3s

| Engine | Image contains | Port | Client in the pod |
|---|---|---|---|
| postgres | `postgres` | 5432 | `psql` |
| mysql | `mysql`, `mariadb` | 3306 | `mysql` |

Read the pod's labels, namespace and the Secret its environment names for the superuser
password. Use `pods_exec` with the client inside the pod; never rely on a client on the host.

## Task database and role

Both are named `task_<task short id>`: letters, digits and `_` only.

- postgres: as the superuser, `DROP DATABASE IF EXISTS`, `DROP ROLE IF EXISTS`, `CREATE ROLE ... LOGIN PASSWORD`, `CREATE DATABASE ... OWNER`. Drop only these two names.
- mysql: `DROP DATABASE IF EXISTS`, `DROP USER IF EXISTS`, `CREATE DATABASE`, `CREATE USER`, `GRANT ALL ON <task database>.*` to that user only.
- SQLite: no engine and no k3s. The skill never writes into a worktree, so it cannot create the file. The database path variable points at `task_<task short id>.db` under the service worktree's data folder. The seed SQL is saved on the task service as the variable `SEED_SQL` (command-service values may contain newlines). The service's command line applies it after the migration step and before the run command, for example `<migrate> && printf '%s' "$SEED_SQL" | sqlite3 "$DB_PATH" && <run>`. Ask when the host has no `sqlite3` client or the application has no migration path. Say in the Seed document that the file lives in the service worktree and is replaced when the branch tip changes.

## Connection variable

Set the variable `.env.example` already names (`DATABASE_URL`, or the host, port, name, user and password keys) to the NodePort host and port, the task database and role.
