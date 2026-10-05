package domain

import (
	"regexp"
	"time"
)

// RefKind is what a ref pointing at a commit is.
type RefKind string

const (
	RefHead   RefKind = "head"   // HEAD, when detached; otherwise HEAD marks a branch
	RefBranch RefKind = "branch" // refs/heads/*
	RefRemote RefKind = "remote" // refs/remotes/*
	RefTag    RefKind = "tag"    // refs/tags/*
)

// GitRef is a name pointing at a commit: a branch, remote branch or tag.
type GitRef struct {
	Name string  `json:"name"`
	Kind RefKind `json:"kind"`
	// Head marks the branch checked out in the repository's own checkout.
	Head bool `json:"head,omitempty"`
}

// Commit is one commit of a repository's history, as its graph needs it.
type Commit struct {
	Hash        string    `json:"hash"`
	Parents     []string  `json:"parents"`
	AuthorName  string    `json:"author_name"`
	AuthorEmail string    `json:"author_email"`
	AuthoredAt  time.Time `json:"authored_at"`
	Subject     string    `json:"subject"`
	Refs        []GitRef  `json:"refs,omitempty"`
}

// FileChange is one file a commit changed: lines added and deleted, both -1
// for a binary file.
type FileChange struct {
	Path    string `json:"path"`
	Added   int    `json:"added"`
	Deleted int    `json:"deleted"`
}

// CommitDetail is a commit with its whole message and the files it changed
// (against its first parent, for a merge).
type CommitDetail struct {
	Commit
	Body  string       `json:"body"`
	Files []FileChange `json:"files"`
}

// MaxHistory bounds how many commits one history read returns.
const MaxHistory = 2000

var commitHash = regexp.MustCompile(`^[0-9a-f]{4,64}$`)

// ValidCommitHash reports whether s is an abbreviated or full commit hash:
// lowercase hex, so it can never be read as a git option or a ref name.
func ValidCommitHash(s string) bool { return commitHash.MatchString(s) }

// WorkingChange is one uncommitted change in a worktree: Status is git's
// two-letter porcelain code ("M ", " M", "A ", "D ", "R ", "??" …). Added
// and Deleted count lines against HEAD; -1 for a binary or untracked file.
type WorkingChange struct {
	Path    string `json:"path"`
	Status  string `json:"status"`
	Added   int    `json:"added"`
	Deleted int    `json:"deleted"`
}

// WorktreeStatus is where a checkout is: its HEAD commit ("" before the
// first), its branch ("" when detached) and what is not committed.
type WorktreeStatus struct {
	Head    string          `json:"head"`
	Branch  string          `json:"branch"`
	Changes []WorkingChange `json:"changes"`
}

// WorkspaceHistory is a session's worktree as history: its branch beside
// the one it was cut from, how far they diverged, and what it has not
// committed yet.
type WorkspaceHistory struct {
	WorkspaceID  string          `json:"workspace_id"`
	RepositoryID string          `json:"repository_id"`
	Branch       string          `json:"branch"`
	Head         string          `json:"head"`
	Base         string          `json:"base"`
	Ahead        int             `json:"ahead"`
	Behind       int             `json:"behind"`
	Commits      []Commit        `json:"commits"`
	Changes      []WorkingChange `json:"changes"`
	// Gone: the worktree is no longer on disk (the session was deleted),
	// so there are no uncommitted changes to show.
	Gone bool `json:"gone,omitempty"`
}
