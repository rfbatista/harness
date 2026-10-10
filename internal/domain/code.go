package domain

import (
	"path"
	"strings"
)

// Limits of the code views: what one answer may carry.
const (
	// MaxCodeFileBytes caps a file's content; past it the content is cut at
	// a line end and marked truncated.
	MaxCodeFileBytes = 1 << 20
	// MaxCodePaths caps a source's path listing.
	MaxCodePaths = 50_000
	// MaxDiffLines caps the changed lines a diff carries hunks for in total,
	// and one file's lines when it is asked for alone.
	MaxDiffLines = 20_000
	// MaxDiffFileLines caps one file's changed lines in a whole diff.
	MaxDiffFileLines = 3_000
	// MaxDiffFileBytes caps one file's patch text.
	MaxDiffFileBytes = 4 << 20
)

// CodeSourceKind says what a code view reads: a repository at a ref, or a
// session's worktree as it is on disk.
type CodeSourceKind string

const (
	CodeSourceRef       CodeSourceKind = "ref"
	CodeSourceWorkspace CodeSourceKind = "workspace"
)

// CodeSource is the source a code view read, after resolving it.
type CodeSource struct {
	Kind         CodeSourceKind `json:"kind"`
	RepositoryID string         `json:"repository_id"`
	WorkspaceID  *string        `json:"workspace_id"`
	// Ref is the ref as asked (or the default); a workspace's branch.
	Ref string `json:"ref"`
	// Commit is the full hash Ref resolved to; a workspace's HEAD.
	Commit string `json:"commit"`
	// Gone: the workspace's worktree is no longer on disk.
	Gone bool `json:"gone"`
}

// Kinds of a tree entry.
const (
	EntryDir       = "dir"
	EntryFile      = "file"
	EntrySymlink   = "symlink"
	EntrySubmodule = "submodule"
)

// File states against a workspace's base, and in a diff.
const (
	ChangeAdded       = "added"
	ChangeModified    = "modified"
	ChangeDeleted     = "deleted"
	ChangeRenamed     = "renamed"
	ChangeCopied      = "copied"
	ChangeTypeChanged = "type_changed"
	ChangeUntracked   = "untracked"
)

// TreeEntry is one direct child of a directory.
type TreeEntry struct {
	Path   string  `json:"path"`
	Name   string  `json:"name"`
	Kind   string  `json:"kind"`
	Size   *int64  `json:"size"`
	Target *string `json:"target"`
	// Change is a workspace file's state against the workspace's base; a
	// directory is "modified" when anything under it changed.
	Change *string  `json:"change"`
	Zones  []string `json:"zones"`
}

// CodeTree is one directory of a source.
type CodeTree struct {
	Source  CodeSource  `json:"source"`
	Path    string      `json:"path"`
	Entries []TreeEntry `json:"entries"`
}

// CodePaths is every file path of a source, for a quick-open finder.
type CodePaths struct {
	Source    CodeSource `json:"source"`
	Paths     []string   `json:"paths"`
	Truncated bool       `json:"truncated"`
}

// CodeFile is one file's content.
type CodeFile struct {
	Source    CodeSource `json:"source"`
	Path      string     `json:"path"`
	Size      int64      `json:"size"`
	Binary    bool       `json:"binary"`
	Truncated bool       `json:"truncated"`
	Language  string     `json:"language"`
	Content   *string    `json:"content"`
	// LineCount counts the lines of Content (0 when binary).
	LineCount int      `json:"line_count"`
	Zones     []string `json:"zones"`
}

// Diff scopes.
const (
	ScopeAll     = "all"
	ScopeBranch  = "branch"
	ScopeWorking = "working"
	ScopeCommit  = "commit"
	ScopeRange   = "range"
)

// Why a file's hunks are not in a diff.
const (
	WithheldTooLarge = "too_large"
	WithheldBinary   = "binary"
)

// CodeDiff is a set of changed files between two sides.
type CodeDiff struct {
	Source CodeSource `json:"source"`
	// Base is the full hash of the base side.
	Base    string     `json:"base"`
	Scope   string     `json:"scope"`
	Files   []FileDiff `json:"files"`
	Added   int        `json:"added"`
	Deleted int        `json:"deleted"`
	// Truncated: hunks were withheld from some file by the totals limit.
	Truncated bool `json:"truncated"`
}

// FileDiff is one changed file. Added and Deleted are -1 for a binary file;
// Hunks is nil when withheld.
type FileDiff struct {
	Path     string   `json:"path"`
	OldPath  *string  `json:"old_path"`
	Status   string   `json:"status"`
	Binary   bool     `json:"binary"`
	Added    int      `json:"added"`
	Deleted  int      `json:"deleted"`
	Hunks    []Hunk   `json:"hunks"`
	Withheld *string  `json:"withheld"`
	Language string   `json:"language"`
	Zones    []string `json:"zones"`
}

// Hunk is one @@ block of a unified diff.
type Hunk struct {
	OldStart int        `json:"old_start"`
	OldLines int        `json:"old_lines"`
	NewStart int        `json:"new_start"`
	NewLines int        `json:"new_lines"`
	Header   string     `json:"header"`
	Lines    []HunkLine `json:"lines"`
}

// Kinds of a hunk line.
const (
	LineContext = "context"
	LineAdd     = "add"
	LineDel     = "del"
)

// HunkLine is one line of a hunk, with its number on either side.
type HunkLine struct {
	Kind      string `json:"kind"`
	Old       *int   `json:"old"`
	New       *int   `json:"new"`
	Text      string `json:"text"`
	NoNewline bool   `json:"no_newline"`
}

// CodeMap is how a source's files divide into bounded contexts and zones.
type CodeMap struct {
	Source           CodeSource     `json:"source"`
	Contexts         []ContextCount `json:"contexts"`
	UnassignedZones  []ZoneCount    `json:"unassigned_zones"`
	UnzonedFileCount int            `json:"unzoned_file_count"`
	FileCount        int            `json:"file_count"`
}

// ContextCount is a bounded context with its zones' file counts.
type ContextCount struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Purpose   string      `json:"purpose"`
	Zones     []ZoneCount `json:"zones"`
	FileCount int         `json:"file_count"`
}

// ZoneCount is a zone with the number of the source's files it matches.
type ZoneCount struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Pattern   string `json:"pattern"`
	FileCount int    `json:"file_count"`
}

// CleanCodePath checks a repository-relative path asked for by a client and
// returns it in its one form ("" for the root). It refuses absolute paths,
// ".." and "." segments, empty segments, NUL bytes, backslashes and any
// ".git" segment, so the path can reach neither git's options nor a file
// outside the source.
func CleanCodePath(p string) (string, bool) {
	p = strings.TrimSuffix(p, "/")
	if p == "" {
		return "", true
	}
	if strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\x00\\") {
		return "", false
	}
	for _, seg := range strings.Split(p, "/") {
		switch seg {
		case "", ".", "..":
			return "", false
		}
		if strings.EqualFold(seg, ".git") {
			return "", false
		}
	}
	return p, true
}

// ValidRefName reports whether r names a ref the code views accept: a
// branch, remote branch, tag, HEAD, a refs/… name or a commit hash. It
// follows git check-ref-format and refuses what git would read as an option
// or a revision expression (~ ^ : @{ ..).
func ValidRefName(r string) bool {
	if r == "" || r == "@" || strings.HasPrefix(r, "-") || strings.HasPrefix(r, "/") ||
		strings.HasSuffix(r, "/") || strings.HasSuffix(r, ".") || strings.HasSuffix(r, ".lock") ||
		strings.Contains(r, "..") || strings.Contains(r, "@{") || strings.Contains(r, "//") {
		return false
	}
	for _, c := range r {
		if c < 0x20 || c == 0x7f || strings.ContainsRune(" ~^:?*[\\", c) {
			return false
		}
	}
	for _, seg := range strings.Split(r, "/") {
		if strings.HasPrefix(seg, ".") || strings.HasSuffix(seg, ".lock") {
			return false
		}
	}
	return true
}

// languages maps a file extension (lowercase, with its dot) to a language id.
var languages = map[string]string{
	".go": "go", ".templ": "templ", ".js": "javascript", ".mjs": "javascript", ".cjs": "javascript",
	".jsx": "jsx", ".ts": "typescript", ".mts": "typescript", ".cts": "typescript", ".tsx": "tsx",
	".json": "json", ".md": "markdown", ".markdown": "markdown", ".html": "html", ".htm": "html",
	".css": "css", ".scss": "scss", ".sass": "sass", ".less": "less", ".yaml": "yaml", ".yml": "yaml",
	".toml": "toml", ".xml": "xml", ".svg": "xml", ".sql": "sql", ".sh": "shell", ".bash": "shell",
	".zsh": "shell", ".py": "python", ".rb": "ruby", ".rs": "rust", ".java": "java", ".kt": "kotlin",
	".kts": "kotlin", ".swift": "swift", ".c": "c", ".h": "c", ".cc": "cpp", ".cpp": "cpp", ".cxx": "cpp",
	".hpp": "cpp", ".cs": "csharp", ".php": "php", ".lua": "lua", ".proto": "protobuf",
	".graphql": "graphql", ".gql": "graphql", ".vue": "vue", ".svelte": "svelte", ".ini": "ini",
	".env": "dotenv", ".txt": "plaintext", ".csv": "csv",
	".dockerfile": "dockerfile", ".tf": "hcl", ".hcl": "hcl", ".ex": "elixir", ".exs": "elixir",
	".erl": "erlang", ".hs": "haskell", ".scala": "scala", ".dart": "dart", ".r": "r", ".pl": "perl",
	".clj": "clojure", ".zig": "zig", ".nix": "nix", ".mk": "makefile",
}

// languageNames maps whole file names that have no telling extension.
var languageNames = map[string]string{
	"Makefile": "makefile", "GNUmakefile": "makefile", "Dockerfile": "dockerfile",
	"go.mod": "gomod", "go.sum": "plaintext", ".gitignore": "ignore", ".dockerignore": "ignore",
	"Gemfile": "ruby", "Rakefile": "ruby", "Justfile": "just", "justfile": "just",
}

// LanguageOf is the language id of a file from its name, "plaintext" when
// nothing tells.
func LanguageOf(p string) string {
	name := path.Base(p)
	if l, ok := languageNames[name]; ok {
		return l
	}
	if strings.HasPrefix(name, "Dockerfile.") {
		return "dockerfile"
	}
	if l, ok := languages[strings.ToLower(path.Ext(name))]; ok {
		return l
	}
	return "plaintext"
}
