package vshell

import (
	"crypto/sha256"
	"encoding/hex"
)

// Version names the content of a file the harness writes that has no
// version of its own: the first 12 hex characters of its sha256, like a
// document's.
func Version(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])[:12]
}

// readme is the shell's README.md: a short map of the layout and its rules.
const readme = `# Virtual shell

This folder holds your task's and your project's resources as files. The
harness keeps it in step with what is stored: what others change appears
here, and what you write here is stored.

    README.md                 this map (read-only)
    .index.json               every file: path, kind, id, scope, version, title, updated_at (read-only)
    task/task.json            the task: id, title, description, status, architect_session_id (read-only)
    task/docs/                documents linked to this task, task scope
    task/comments/            one file per comment, body only
    task/resources/           the person's uploads under their names (read-only)
    task/resources/links.md   the links the person added (read-only)
    task/designs/             artifacts published on this task, file bytes
    task/designs/urls.md      dev-server URLs published on this task (read-only)
    task/designs/attached.md  project assets attached to this task: title → path here (read-only)
    project/docs/             every project document
    project/designs/          every project design asset, file bytes
    project/designs/urls.md   project design assets that are URLs (read-only)

File names are <slug of title>.<ext>, with -<short id> added only on a
clash; a file you create keeps the name you chose. A document's extension
is its format: .html/.htm html, .md markdown, anything else text.

Comments are named <yyyymmdd-hhmmss>-<author>-<short id>.md. A file you
create in task/comments is your comment. You can edit or delete only your
own; the others' are read-only (mode 0444), like every read-only path above.

A file you create, change or remove in task/designs publishes, revises or
unpublishes a design; mv it to project/designs to make it a project asset,
and back to make it the task's again. Folders under designs are kept.
`
