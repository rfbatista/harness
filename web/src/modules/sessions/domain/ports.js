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
 * @property {string} ticketId      "" starts it on the project with no task: accepted only with mode "product"
 * @property {string} repositoryId  where its worktree is cut
 * @property {string} agentId       "" runs plain claude
 * @property {""|"architect"|"implement"|"design"|"product"} [mode] a role on top of the agent: "architect" grills the person, shapes the task into specs and delegates them; "implement" turns a Spec task document into reviewed tickets and implements them; "design" publishes components and media to the Design tab; "product" interviews the person, writes a PRD and creates child tasks with briefs, never code
 * @property {string} prompt        the optional first message; "" opens claude waiting for you
 * @property {"off"|"edits"|"all"} autoAccept  which tool calls run without asking
 * @property {string} [baseBranch]  the branch its worktree branches off; "" means the checkout's HEAD
 * @property {string} [branchName]  the name of the session's own branch; "" lets the server generate one
 * @property {readonly string[]} [bringUnderSessionIds]  with mode "architect": sessions of the task to move under the new architect as it starts
 *
 * @typedef {object} Brought  what bringing sessions under the architect did
 * @property {Session} architect
 * @property {readonly Session[]} moved  now its delegates, in request order
 * @property {readonly { sessionId: string, reason: "not_on_task"|"is_architect"|"already_under"|"not_found" }[]} skipped
 *
 * @typedef {object} BranchNameCheck  what the server says about a branch name for a new session
 * @property {string} name        the name checked, trimmed; "" when none was given
 * @property {string} generated   the name the server would generate; the form prefills it
 * @property {boolean} valid      a valid git branch name
 * @property {boolean} available  valid and no branch of that name exists
 * @property {""|"BRANCH_NAME_INVALID"|"BRANCH_EXISTS"} code
 * @property {string} reason      what is wrong; "" when available
 * @property {string} suggestion  the first free variant, when the name exists; else ""
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
 *           INVALID_INPUT (no repository, or no task in a mode other than
 *           product), BRANCH_NAME_INVALID or BRANCH_EXISTS (details name the
 *           name and a free suggestion), PROJECT_NOT_FOUND, TICKET_NOT_FOUND,
 *           CROSS_PROJECT_ACCESS, CLAUDE_CLI_NOT_FOUND or
 *           SERVER_HOSTING_UNAVAILABLE.
 * @property {(repositoryId: string) => Promise<{ fetchedAt: Date }>} fetchRemote
 *           Fetches the repository's origin with the stored credential, pruning what is gone
 *           there; the next listBranches shows the result. Rejects with REPOSITORY_NOT_FOUND,
 *           NO_REMOTE, GIT_CREDENTIAL_MISSING, GIT_CREDENTIAL_REJECTED,
 *           GIT_CREDENTIAL_STORE_LOCKED or FETCH_FAILED.
 * @property {(repositoryId: string) => Promise<Branch[]>} listBranches
 *           The branches a new session can branch off: local ones, then
 *           remote ones. Rejects with REPOSITORY_NOT_FOUND.
 * @property {(query: { repositoryId: string, name: string, baseBranch?: string, ticketId?: string }, signal?: AbortSignal) => Promise<BranchNameCheck>} checkBranchName
 *           Reads git only: suggests the generated name and says whether a
 *           name is valid and free. Rejects with REPOSITORY_NOT_FOUND.
 * @property {(request: { ticketId: string, sessionIds: readonly string[] }) => Promise<Brought>} bringUnder
 *           Moves the sessions under the task's running architect; the ones
 *           that cannot move are skipped. Rejects with NO_ARCHITECT,
 *           NOTHING_TO_MOVE (details: the skipped list), TICKET_NOT_FOUND or
 *           INVALID_INPUT.
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
 * @property {(projectId: string, repositoryId?: string) => Promise<AgentChoices>} agentChoices
 *           The agents the project enables, its mode and the catalog size:
 *           what the new-session and brainstorm pickers offer. With a
 *           repository, only the agents assigned to it, and notOffered says why
 *           each of the others is missing there.
 * @property {(handlers: { onEvent: (data: unknown) => void,
 *             onStatus?: (status: FeedStatus) => void }) => () => void} followAgents
 *           Follows the page feed messages (agent_catalog_changed among them)
 *           until the returned function is called.
 */

/**
 * @typedef {object} AgentChoices
 * @property {"all" | "selected"} mode
 * @property {{ id: string, name: string, description: string, repositoryPromptSet?: boolean }[]} agents  the enabled agents
 * @property {number} total  how many agents the catalog has
 * @property {{ name: string, reason: "unassigned" | "not_enabled" }[]} [notOffered]  with a repository: the rest of the catalog
 */

/**
 * Merging a session's branch into its repository's default branch, in the
 * local checkout, which the harness server does (ADR 0021). Implemented by
 * infrastructure/merge-gateway.js (GET and POST /api/sessions/{id}/merge).
 *
 * @typedef {import("./session.js").MergeResult} MergeResult
 * @typedef {{ name: string, isDefault: boolean, checkedOut: boolean, dirty: boolean }} MergeTarget
 *          a local branch a merge can go into; dirty: checked out in the main checkout with uncommitted changes
 * @typedef {object} MergeOffer  whether a merge into `into` applies now, and what it would do
 * @property {boolean} available
 * @property {string} branch               the session's branch
 * @property {string} into                 the target the offer is about
 * @property {string} reason               the MERGE_* code saying why not; "" when available
 * @property {number} ahead                commits the branch has that `into` lacks
 * @property {number} uncommittedChanges   files changed in the session's worktree and not committed; a warning, never a block
 * @property {string} suggestedMessage     the squash message suggestion
 * @property {readonly MergeTarget[]} targets  every local branch but the session's own, the default first
 *
 * @typedef {object} MergeGateway
 * @property {(sessionId: string, target?: string, signal?: AbortSignal) => Promise<MergeOffer>} offer
 *           The offer for a target ("" is the default branch). Rejects with SESSION_NOT_FOUND.
 * @property {(sessionId: string, request?: { target?: string, method?: "merge" | "squash", message?: string }) => Promise<MergeResult>} merge
 *           Merges into the target with --no-ff, or squashes it into one commit with the message. A refusal rejects with its code and reason
 *           (MERGE_CHECKOUT_DIRTY, MERGE_WRONG_BRANCH, MERGE_ALREADY_MERGED,
 *           MERGE_CONFLICT, …); it is on the session's lastMerge too.
 */

/**
 * The architect channel on a task: the messages between its architect and
 * the delegates, and the status-check loops on them. Implemented by
 * infrastructure/channel-gateway.js (the Web UI contract's routes and the
 * project feed) and infrastructure/memory-channel.js (tests and web/dev).
 * Both run testing/channel-contract.js.
 *
 * @typedef {import("./channel.js").TaskMessage} TaskMessage
 * @typedef {import("./channel.js").StatusCheck} StatusCheck
 * @typedef {{ kind: "message", message: TaskMessage }} ChannelEvent
 *          a message was sent, or became delivered (compare by id)
 *
 * @typedef {object} ChannelGateway
 * @property {(filter: { ticketId: string, sessionId?: string, since?: Date | null }, signal?: AbortSignal) => Promise<TaskMessage[]>} listMessages
 *           The task's messages, oldest first: from or to sessionId when set,
 *           created after since when set.
 * @property {(delegateSessionId: string, everyMinutes: number) => Promise<StatusCheck>} setStatusCheck
 *           0 pauses the delegate's loop; a value resumes or retunes it.
 *           Rejects with STATUS_CHECK_NOT_FOUND (the delegate has no loop: a
 *           person cannot create one) or INVALID_INPUT (outside 2–240).
 * @property {(projectId: string,
 *             onEvent: (event: ChannelEvent) => void,
 *             onStatus: (status: FeedStatus) => void) => () => void} follow
 *           Follows the project's task messages until the returned function
 *           is called. Other feed messages are not delivered.
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
 * The person's own terminals on a session's virtual file system. Implemented
 * by infrastructure/person-terminals-gateway.js (the person-terminals routes and the
 * session feed's `person_terminal_ended`) and infrastructure/memory-person-terminals.js.
 * Attaching to one is a TerminalGateway at /api/person-terminals/{id}/terminal.
 *
 * @typedef {{ id: string, sessionId: string, root: string, dir: string, label: string, openedAt: Date }} PersonTerminal
 * @typedef {{ id: string, sessionId: string, reason: string, message: string }} PersonTerminalEnded
 *
 * @typedef {object} PersonTerminalGateway
 * @property {(sessionId: string, signal?: AbortSignal) => Promise<PersonTerminal[]>} list  the open ones, oldest first
 * @property {(sessionId: string, at: { root: string, dir: string, cols: number, rows: number }) => Promise<PersonTerminal>} open
 *           Rejects with the server's refusal: TERMINAL_SERVER_EXPOSED, TERMINAL_HOST_REFUSED,
 *           TERMINAL_ORIGIN_REFUSED, TERMINAL_READ_ONLY_UNAVAILABLE, ROOT_GONE, PATH_NOT_FOUND, INVALID_INPUT.
 * @property {(id: string) => Promise<void>} close  hangs up; one that ended is not an error
 * @property {(sessionId: string, onEnded: (e: PersonTerminalEnded) => void, onStatus?: (s: import("../../../shared/domain/feed.js").FeedStatus) => void) => () => void} followEnded
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
 * @typedef {{ kind: "changed", artifact: Artifact }
 *   | { kind: "deleted", id: string, projectId: string, ticketId: string, attachedTicketIds: string[] }} ProjectArtifactChange
 *           An `artifact` change on the project feed: after an attach, a
 *           detach, a scope move or a re-publish of a project asset (changed),
 *           or after a delete (deleted, with the ids as they were before).
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
 *           whose worktree is gone). A move back to the task detaches it
 *           from every task; either move is also a change on the project feed.
 * @property {(artifactId: string) => Promise<void>} remove
 *           Deletes the artifact in either scope, and its attachments with it.
 *           Rejects with ARTIFACT_NOT_FOUND.
 * @property {(rows: unknown[]) => Artifact[]} decodeArtifacts
 *           Artifacts a page was seeded with, in the API's wire shape.
 *           Throws BAD_RESPONSE on rows it cannot read.
 * @property {(ticketId: string, signal?: AbortSignal) => Promise<Artifact[]>} listTask
 *           The task's design assets, most recently updated first: what its
 *           sessions produced (either scope) and the project assets attached
 *           to it.
 * @property {(artifactId: string, ticketId: string) => Promise<Artifact>} attach
 *           Attaches a project asset to another task of its project and
 *           returns it. Attaching again, or to the producing task, changes
 *           nothing. Revision and updatedAt never change. Rejects with
 *           ARTIFACT_NOT_FOUND, TICKET_NOT_FOUND, INVALID_INPUT (a missing
 *           id), ARTIFACT_NOT_IN_PROJECT (a task-scope artifact) or
 *           ARTIFACT_PROJECT_MISMATCH (a task of another project).
 * @property {(artifactId: string, ticketId: string) => Promise<Artifact>} detach
 *           Detaches it from a task and returns it. Detaching a task it is
 *           not attached to changes nothing. Rejects with ARTIFACT_NOT_FOUND,
 *           INVALID_INPUT or ARTIFACT_PRODUCER_TASK (its own task).
 * @property {(projectId: string,
 *             onChange: (change: ProjectArtifactChange) => void,
 *             onStatus: (status: FeedStatus) => void) => () => void} followProject
 *           Follows the project feed for artifact changes until the returned
 *           function is called. The feed's other keys are not delivered.
 * @property {(sessionId: string,
 *             onEvent: (event: ArtifactEvent) => void,
 *             onStatus: (status: FeedStatus) => void) => () => void} follow
 *           Follows the session's event stream for publishes until the
 *           returned function is called. Other event types on the stream are
 *           not delivered.
 */

/**
 * A session's saved conversation, read a page at a time (the contract
 * "Harness server ↔ Web UI — agent session history", T1–T6). Implemented by
 * infrastructure/transcript-gateway.js (GET /api/sessions/{id}/transcript)
 * and infrastructure/memory-transcripts.js (tests and web/dev). Both run
 * testing/transcript-contract.js.
 *
 * @typedef {import("./transcript.js").TranscriptPage} TranscriptPage
 *
 * @typedef {object} TranscriptGateway
 * @property {(sessionId: string, page?: { before?: string, after?: string, limit?: number }, signal?: AbortSignal) => Promise<TranscriptPage>} page
 *           No cursor: the newest `limit` entries. before: the `limit`
 *           entries just older than that cursor. after: the entries newer
 *           than it, oldest first, up to `limit`. Rejects with
 *           SESSION_TRANSCRIPT_MISSING (no saved conversation yet),
 *           SESSION_NOT_FOUND or INVALID_INPUT (a bad cursor or limit, or
 *           both cursors).
 * @property {(sessionId: string, since?: { afterSeq?: number }, signal?: AbortSignal) => Promise<{ events: { seq: number, type: string, at: string, payload: any }[], lastSeq: number }>} [events]
 *           The session's event log after `afterSeq`, for the lines a
 *           restart adds to its history (restartMarkers). Optional: without
 *           it the History tab shows the conversation alone.
 */

export {};

/**
 * A session's sandbox violations, newest first. Implemented by
 * infrastructure/sandbox-gateway.js (GET /api/sessions/{id}/sandbox/violations).
 *
 * @typedef {import("./sandbox.js").ViolationLog} ViolationLog
 *
 * @typedef {object} SandboxGateway
 * @property {(sessionId: string, signal?: AbortSignal) => Promise<ViolationLog>} violations
 *           Rejects with SESSION_NOT_FOUND. `available` is false where the
 *           platform collects none (Linux).
 */

/**
 * A session's interviews, read and followed. Implemented by
 * infrastructure/interviews-gateway.js (GET /api/sessions/{id}/interviews and
 * the session's interview feed).
 *
 * @typedef {import("./interviews.js").InterviewList} InterviewList
 * @typedef {{ seq: number, kind: "interview_asked" | "interview_changed", interview: import("./interviews.js").Interview }} InterviewEvent
 *
 * @typedef {object} InterviewGateway
 * @property {(sessionId: string, signal?: AbortSignal) => Promise<InterviewList>} list
 *           The seed's body again, for a resync.
 * @property {(sessionId: string,
 *             onEvent: (event: InterviewEvent) => void,
 *             onStatus: (status: FeedStatus) => void) => () => void} follow
 *           Follows the session's interview feed until the returned function
 *           is called. An event it cannot read is dropped.
 * @property {(id: string, answers: import("./interviews.js").Answer[]) => Promise<import("./interviews.js").Interview>} submit
 *           Sends the answers, one per question. Idempotent per interview id: a
 *           repeat returns the stored interview and delivers nothing. Refusals
 *           are StructuredErrors: INTERVIEW_EXPIRED and INTERVIEW_NOT_OPEN carry
 *           the current interview in `details.interview` (read into the model),
 *           INTERVIEW_ANSWERS_INVALID and INTERVIEW_NOT_FOUND only their message.
 * @property {(id: string, note: string) => Promise<import("./interviews.js").Interview>} dismiss
 *           Closes the interview without answers, with an optional note. As submit.
 */
