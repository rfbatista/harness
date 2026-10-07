// What the sessions module needs from the outside world.
//
// Implemented by infrastructure/sessions-gateway.js (over /api) and
// infrastructure/memory-gateway.js (in memory, for tests and web/dev).
// Both run testing/gateway-contract.js.

/**
 * @typedef {import("./session.js").Session} Session
 * @typedef {import("./session.js").SessionChange} SessionChange
 * @typedef {import("../../../shared/domain/feed.js").FeedStatus} FeedStatus
 *
 * @typedef {object} SessionGateway
 * @typedef {{ projectId: string, ticketId?: string }} SessionFilter
 *
 * @typedef {object} StartRequest  an interactive session on a task, run in a PTY on the server
 * @property {string} projectId
 * @property {string} ticketId
 * @property {string} repositoryId  where its worktree is cut
 * @property {string} agentId       "" runs plain claude
 * @property {""|"architect"|"design"} [mode] a role on top of the agent: "architect" shapes the task into specs and delegates them; "design" publishes components and media to the Design tab
 * @property {string} prompt        the optional first message; "" opens claude waiting for you
 * @property {"off"|"edits"|"all"} autoAccept  which tool calls run without asking
 * @property {string} [baseBranch]  the branch its worktree branches off; "" means the checkout's HEAD
 *
 * @typedef {object} Branch  a ref a session's worktree can branch off
 * @property {string} name     e.g. main, origin/main
 * @property {boolean} remote  a remote-tracking ref
 * @property {boolean} isHead  the branch checked out in the repository
 * @property {{ cols: number, rows: number }} size  the terminal size it starts at
 *
 * @property {(seed: unknown) => { projectId: string, ticketId: string, sessions: Session[] }} decodeSeed
 *           Reads the page seed the server embedded (same JSON shape as the
 *           API). Throws BAD_RESPONSE on a malformed seed.
 * @property {(filter: SessionFilter, signal?: AbortSignal) => Promise<Session[]>} list
 *           A project's sessions, or one task's when ticketId is set.
 *           Rejects with PROJECT_NOT_FOUND.
 * @property {(request: StartRequest) => Promise<Session>} start
 *           Starts an interactive session on the server's terminal host: cuts
 *           its worktree and branch, then runs claude in a PTY. Rejects with
 *           INVALID_INPUT (no repository), PROJECT_NOT_FOUND, TICKET_NOT_FOUND,
 *           CROSS_PROJECT_ACCESS, CLAUDE_CLI_NOT_FOUND or
 *           SERVER_HOSTING_UNAVAILABLE.
 * @property {(repositoryId: string) => Promise<Branch[]>} listBranches
 *           The branches a new session can branch off: local ones, then
 *           remote ones. Rejects with REPOSITORY_NOT_FOUND.
 * @property {(sessionId: string, size?: { cols: number, rows: number }) => Promise<Session>} resume
 *           Brings an ended interactive session back on the server's terminal
 *           host: same id, branch, worktree and conversation. Followers see it
 *           running again. Rejects with SESSION_NOT_FOUND,
 *           SESSION_NOT_INTERACTIVE, SESSION_ALREADY_RUNNING (it never ended, or
 *           someone resumed it first), WORKSPACE_MISSING,
 *           SESSION_TRANSCRIPT_MISSING or INVALID_INPUT.
 * @property {(sessionId: string) => Promise<void>} stop
 *           Stops the session. Rejects with SESSION_NOT_FOUND.
 * @property {(sessionId: string) => Promise<void>} remove
 *           Deletes the session: stops it if it runs, removes its worktree
 *           (never its branch) and its record; followers get a deletion.
 *           Rejects with SESSION_NOT_FOUND.
 * @property {(projectId: string,
 *             onChange: (change: SessionChange) => void,
 *             onStatus: (status: FeedStatus) => void) => () => void} follow
 *           Follows the project's changes until the returned function is called.
 */

/**
 * A session's terminal, attached over the socket the server keeps for it.
 * Implemented by infrastructure/terminal-gateway.js (WebSocket) and
 * infrastructure/memory-terminals.js (tests and web/dev).
 *
 * @typedef {object} TerminalSnapshot  the screen as it is when attaching
 * @property {string} screen   one line per row, with its styles (SGR sequences)
 * @property {string} scrollback  the main screen's lines that scrolled off the top, oldest first, rendered
 *                                like screen; "" when there is no history: the alternate screen, a log, a
 *                                server that has none, or one that does not send the field yet
 * @property {number} cursorX
 * @property {number} cursorY
 * @property {boolean} altScreen
 * @property {number} cols
 * @property {number} rows
 * @property {string} title
 * @property {boolean} log     screen is raw output to write as printed (an application run's log), with no cursor to place
 *
 * @typedef {object} TerminalHandlers
 * @property {() => void} [onOpen]
 * @property {(snapshot: TerminalSnapshot) => void} onSnapshot  first, and again whenever the screen is redrawn from scratch
 * @property {(bytes: Uint8Array) => void} onOutput              everything printed after the snapshot, in order
 * @property {(title: string) => void} [onTitle]
 * @property {(code: number) => void} [onExit]                   the process is gone
 * @property {() => void} [onClosed]                             the connection ended (after an exit, or dropped)
 *
 * @typedef {object} TerminalConnection
 * @property {(event: KeyboardEvent) => boolean} key  sends a key press; false when it is not one to send (the browser keeps it)
 * @property {(text: string) => void} paste
 * @property {(size: { cols: number, rows: number }) => void} resize
 * @property {() => void} resync                      asks for a fresh snapshot
 * @property {() => void} close                       detaches; the session keeps running
 *
 * @typedef {object} TerminalGateway
 * @property {(sessionId: string, handlers: TerminalHandlers) => TerminalConnection} attach
 */

/**
 * A session's published artifacts and the stream that announces them.
 * Implemented by infrastructure/artifacts-gateway.js (GET /api/artifacts and
 * the session's SSE stream) and infrastructure/memory-artifacts.js (tests and
 * web/dev). Both run testing/artifact-contract.js.
 *
 * @typedef {import("./artifact.js").Artifact} Artifact
 * @typedef {{ kind: "published", artifact: Artifact } | { kind: "ended" }} ArtifactEvent
 *           published: a first publish, a re-publish (compare revision) or a
 *           move between scopes (same revision, later updatedAt);
 *           ended: the session is over, no more publishes will come.
 *
 * @typedef {object} ArtifactGateway
 * @property {(sessionId: string, signal?: AbortSignal) => Promise<Artifact[]>} list
 *           The session's artifacts, most recently updated first. A session
 *           without any, or an unknown one, lists [].
 * @property {(projectId: string, signal?: AbortSignal) => Promise<Artifact[]>} listProject
 *           The project's project-scoped artifacts, most recently updated
 *           first, whatever session produced them.
 * @property {(artifactId: string, scope: import("./artifact.js").ArtifactScope) => Promise<Artifact>} setScope
 *           Moves an artifact between task and project scope and returns it:
 *           the same id and revision, a later updatedAt. The same scope again
 *           changes nothing. The move is announced on the producing session's
 *           stream as a publish of the moved artifact. Rejects with
 *           ARTIFACT_NOT_FOUND, INVALID_INPUT (unknown scope) or
 *           ARTIFACT_NOT_PROMOTABLE (no file to keep: a url, or a session
 *           whose worktree is gone).
 * @property {(artifactId: string) => Promise<void>} remove
 *           Deletes the artifact in either scope. Rejects with ARTIFACT_NOT_FOUND.
 * @property {(sessionId: string,
 *             onEvent: (event: ArtifactEvent) => void,
 *             onStatus: (status: FeedStatus) => void) => () => void} follow
 *           Follows the session's event stream for publishes until the
 *           returned function is called. Other event types on the stream are
 *           not delivered.
 */

export {};
