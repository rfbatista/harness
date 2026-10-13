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

## Writing

Write here with Write, Edit, MultiEdit or NotebookEdit, or with sed -i,
tee, cp, mv, rm, touch and mkdir: each change is checked before it lands
and stored once it has. Nothing else is seen, so nothing else is stored.
A refusal starts with its code:

    BASE_VERSION_STALE  the file changed since you got it; it has been refreshed: read it again and redo the change
    READ_ONLY           the path is read-only
    COMMENT_NOT_YOURS   the comment is someone else's
    NOT_ON_TASK         the document is not linked to this task, so it cannot move to task/docs
    NOT_STORED          the path is outside the folders the harness stores
    REDIRECT_DENIED     shell redirects (>, >>, &>) into this folder are not stored: use Write/Edit or tee
    SHELL_UNAVAILABLE   the harness could not be reached: redo the change once it is back

Redirects are recognised when their target names this folder directly, or
through $HARNESS_SHELL, $HOME, ~ or a cd earlier in the line. A target
reached through eval, another variable or any other indirection is not
recognised: the file changes on disk and is not stored.

The commands' common options are understood; abbreviated long options,
options after the file names, and cp -R src/ with a trailing slash are not,
so write those another way.
`
