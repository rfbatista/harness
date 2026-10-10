// What the code module needs from the outside world. CodeGateway is
// implemented by infrastructure/code-gateway.js (the contract's /api/code/*
// and the project feed) and infrastructure/memory-gateway.js (tests and
// web/dev); both run testing/gateway-contract.js. Highlighter is implemented
// by infrastructure/hljs-highlighter.js and infrastructure/plain-highlighter.js.

/**
 * @typedef {import("./code.js").CodeSource} CodeSource
 * @typedef {import("../../../shared/domain/feed.js").FeedStatus} FeedStatus
 *
 * @typedef {{ repositoryId?: string, ref?: string, workspaceId?: string }} SourceQuery
 *   exactly one of repositoryId (at ref, the checkout's branch when empty) or workspaceId
 *
 * @typedef {{ workspaceId?: string, scope?: string, repositoryId?: string, commit?: string, base?: string, head?: string, path?: string }} DiffQuery
 *   workspaceId with scope (all when empty), repositoryId with commit, or repositoryId with base and head; path narrows it to one file
 *
 * @typedef {object} CodeGateway
 * @property {(src: SourceQuery, dir: string, signal?: AbortSignal) => Promise<import("./code.js").CodeTree>} tree
 * @property {(src: SourceQuery, signal?: AbortSignal) => Promise<import("./code.js").CodePaths>} paths
 * @property {(src: SourceQuery, path: string, signal?: AbortSignal) => Promise<import("./code.js").CodeFile>} file
 * @property {(q: DiffQuery, signal?: AbortSignal) => Promise<import("./code.js").CodeDiff>} diff
 * @property {(src: SourceQuery, signal?: AbortSignal) => Promise<import("./code.js").CodeMap>} map
 *           Each rejects with a StructuredError carrying the contract's code
 *           (INVALID_INPUT, REPOSITORY_NOT_FOUND, WORKSPACE_NOT_FOUND,
 *           WORKSPACE_MISSING, COMMIT_NOT_FOUND, REF_NOT_FOUND,
 *           PATH_NOT_FOUND, UNAVAILABLE).
 * @property {(projectId: string,
 *             onCodeChanged: (change: import("./code.js").CodeChanged) => void,
 *             onStatus: (status: FeedStatus) => void) => () => void} follow
 *           Every code_changed of the project's workspaces until the returned
 *           function is called. Other feed messages are not delivered.
 * @property {(raw: unknown) => CodeSeed} decodeSeed
 *
 * @typedef {object} CodeSeed  what a page's first paint read, in the wire shape, decoded
 * @property {string} projectId
 * @property {import("./routes.js").CodeRoute} route
 * @property {SourceQuery} source           what the page reads
 * @property {string} repositoryName
 * @property {string} branch                a session's branch ("" otherwise)
 * @property {string} baseRef               a session's base branch ("" when unknown)
 * @property {boolean} live                 a running session
 * @property {{ id: string, name: string }[]} worktrees  a session's, when it has several
 * @property {import("./code.js").CodeTree[]} trees      the tree levels painted, root first
 * @property {import("./code.js").CodeFile|null} file
 * @property {import("./code.js").CodeDiff|null} diff
 * @property {import("./code.js").CodeMap|null} map
 * @property {{ code: string, message: string } | null} error   what the first paint hit
 * @property {string[]} refs                 the ref select's options (repository page)
 * @property {string} historyHref
 * @property {string} commit                 a history page's open commit ("" for uncommitted changes)
 * @property {boolean} gone                  the worktree is no longer on disk
 * @property {boolean} baseGone              the worktree's base branch no longer resolves
 *
 * @typedef {{ text: string, tok: string }} Token  tok: a contract tok-* name without the prefix, or ""
 *
 * @typedef {object} Highlighter
 * @property {(language: string) => boolean} supports
 * @property {(language: string, text: string) => Token[][]} tokenize
 *           One array of tokens per line of text (split on "\n").
 */

export {};
