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

This folder holds your task's, your project's and the project's other
tasks' resources as files. The harness keeps it in step with what is
stored: what others change appears here, and what you write here is stored.

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
    tasks/<task-id>/docs/     another task of the project: its documents, task scope

File names are <slug of title>.<ext>, with -<short id> added only on a
clash; a file you create keeps the name you chose. A document's extension
is its format: .html/.htm html, .md markdown, anything else text.

Comments are named <yyyymmdd-hhmmss>-<author>-<short id>.md. A file you
create in task/comments is your comment. You can edit or delete only your
own; the others' are read-only (mode 0444), like every read-only path above.

A person moves documents to project level: moving one from task/docs to
project/docs is refused. Moving one from project/docs to task/docs makes
it this task's, when it is linked to this task.

Every other task of the project has a folder tasks/<task-id>/docs/ (ls
tasks/ lists their ids), with the rules of task/docs/: a file you write
there is a document on that task, titled from its first heading, and a
file you change or remove changes or deletes that task's document. Use it
to write onto a task that is not yours: a brief onto a child task you
created, a PRD onto the feature task. Your own task's documents are in
task/docs/, not under tasks/. A document stays on its task: moving one to
another task's docs is refused, as is a task that does not exist or is in
another project.

A session started on the project with no task (a product session defining
a feature) has no task/ folder at all, only project/ and tasks/: it creates
the feature task with create_task and writes its PRD and each child's brief
under tasks/<task-id>/docs/. A write under task/ there is NOT_STORED.

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
    NOT_ON_TASK         the document is not linked to that task, so it cannot move to its docs
    PROJECT_MOVE_DENIED only a person moves a document to project/docs: ask them to
    TASK_MOVE_DENIED    a document stays on its task: write it onto the other task instead
    TASK_NOT_FOUND      tasks/<task-id> names no task of this project
    TASK_NOT_ON_PROJECT tasks/<task-id> names a task of another project
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
