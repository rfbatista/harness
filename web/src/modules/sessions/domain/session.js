// Sessions: the rules that hold however they are shown. Mirrors
// internal/domain/session.go; no I/O, no Alpine, no DOM.

/**
 * @typedef {"starting"|"running"|"thinking"|"idle"|"waiting_approval"|"paused"|"done"|"failed"|"stopped"} SessionStatus
 *
 * @typedef {object} Session
 * @property {string} id
 * @property {string} projectId
 * @property {string} repositoryId      the repository its worktree was cut from; "" for none
 * @property {string} parentSessionId   the session that started it on its task; "" when a person did
 * @property {string} workspaceId       its git worktree; "" when it was not started on a repository
 * @property {string} branch            the branch its worktree holds; "" for none
 * @property {string} workingDir        where it runs: its worktree
 * @property {string} ticketId          the task it works on; "" for none. A task can run several sessions at once.
 * @property {string} task              what the session was asked to do
 * @property {string} agentId           "" when no agent persona is attached
 * @property {""|"architect"|"design"} mode  the role it was started in on top of its agent; "" for none
 * @property {SessionStatus} status
 * @property {number} pendingApprovals  tool calls waiting for the developer
 * @property {string} lastAction        "" until the agent has done something
 * @property {boolean} interactive       claude runs in a terminal (a PTY) rather than over stream-json
 * @property {"server"|"tui"|""} runsOn  where an interactive session's terminal lives; "" when headless
 * @property {string} runnerHost         the machine of a "tui" session
 * @property {boolean} resumable         a resume would be accepted now (advisory: the call can still be refused)
 * @property {string} resumeBlocked      "" when resumable; otherwise the error code saying why not
 * @property {Role} role                 what it is on its task, derived by the server
 * @property {string} architectSessionId the task's architect; "" when the task has none
 * @property {import("./channel.js").StatusCheck | null} statusCheck  the loop checking on it, on a delegate the architect started
 * @property {Date} updatedAt
 *
 * @typedef {"architect"|"delegate"|""} Role  "" is a peer: started by a person, or outside the architect's tree
 *
 * @typedef {{ kind: "upsert", session: Session } | { kind: "deleted", id: string }} SessionChange
 *
 * @typedef {"architect"|"needs-you"|"active"|"finished"} SessionGroupKey
 * @typedef {{ key: SessionGroupKey, sessions: Session[], depth?: Record<string, number> }} SessionGroup
 *          depth: on the architect group, how far each session is below the architect
 */

export const Status = Object.freeze({
  STARTING: "starting",
  RUNNING: "running",
  THINKING: "thinking",
  IDLE: "idle",
  WAITING_APPROVAL: "waiting_approval",
  PAUSED: "paused",
  DONE: "done",
  FAILED: "failed",
  STOPPED: "stopped",
});

export const STATUSES = Object.freeze(Object.values(Status));

/** The roles a session can be started in on top of its agent. */
export const MODES = Object.freeze(["", "architect", "design"]);

/** The process behind the session is gone for good (domain.SessionStatus.IsTerminal). */
export const isTerminal = (session) =>
  session.status === Status.DONE || session.status === Status.FAILED || session.status === Status.STOPPED;

/** An agent is working on it right now. */
export const isActive = (session) =>
  session.status === Status.STARTING || session.status === Status.RUNNING || session.status === Status.THINKING;

/**
 * The session is waiting on the developer: an approval, or — for an
 * interactive session at its turn boundary (idle) — the next message.
 */
export const needsYou = (session) =>
  !isTerminal(session) &&
  (session.status === Status.WAITING_APPROVAL || session.status === Status.IDLE || session.pendingApprovals > 0);

/** The terminal of this session can be attached from here: it runs on the server and is alive. */
export const hasLiveTerminal = (session) => session.interactive && session.runsOn === "server" && !isTerminal(session);

/** The server says a resume would be accepted now. Advisory: the call can still be refused. */
export const canResume = (session) => session.resumable === true;

/** The size a terminal starts at before its pane measures it; the pane resizes it once attached. */
export const INITIAL_TERMINAL_SIZE = Object.freeze({ cols: 120, rows: 32 });

/**
 * The list after one change from the feed. The changed session moves to the
 * top. `keep` says which sessions the list holds (a task page keeps its own
 * task's): an upsert of any other session leaves it out, so a session that
 * stops matching drops from the list.
 */
export function applyChange(list, change, keep = () => true) {
  if (change.kind === "deleted") return list.filter((s) => s.id !== change.id);
  const rest = list.filter((s) => s.id !== change.session.id);
  return keep(change.session) ? [change.session, ...rest] : rest;
}

/** The predicate a task's page keeps its sessions with. */
export const ofTask = (ticketId) => (session) => session.ticketId === ticketId;

/** Most recently updated first. Returns a new array. */
export const byRecent = (list) => [...list].sort((a, b) => b.updatedAt.getTime() - a.updatedAt.getTime());

/**
 * Sessions grouped the way the developer triages them: on a task with an
 * architect, the architect and its delegates first (the architect group);
 * then what needs them, what is running, what is over. Empty groups are left
 * out. Mirrors group (internal/adapter/in/web/sessions/view.go);
 * web/testdata/views/session-roles.json pins both.
 * @returns {SessionGroup[]}
 */
export function group(list) {
  const recent = byRecent(list);
  const lead = architectGroup(recent);
  const inLead = new Set(lead?.sessions.map((s) => s.id));

  const groups = { "needs-you": [], active: [], finished: [] };
  for (const s of recent) {
    if (inLead.has(s.id)) continue;
    if (needsYou(s)) groups["needs-you"].push(s);
    else if (isTerminal(s)) groups.finished.push(s);
    else groups.active.push(s);
  }
  const triage = Object.entries(groups)
    .filter(([, sessions]) => sessions.length > 0)
    .map(([key, sessions]) => ({ key, sessions }));
  return lead ? [lead, ...triage] : triage;
}

/**
 * The architect and its delegates, depth-first, newest first among siblings.
 * A delegate whose parent is not in the list (deleted) sits right under the
 * architect. Null when no session is the architect.
 * @param {Session[]} recent  most recently updated first
 * @returns {SessionGroup | null}
 */
function architectGroup(recent) {
  const architect = recent.find((s) => s.role === "architect");
  if (!architect) return null;
  const delegates = recent.filter((s) => s.role === "delegate");
  const ids = new Set(delegates.map((s) => s.id));
  const parentOf = (s) => (s.parentSessionId && ids.has(s.parentSessionId) ? s.parentSessionId : architect.id);

  const sessions = [];
  const depth = {};
  const visit = (s, d) => {
    if (s.id in depth) return; // a cycle; each session shows once
    sessions.push(s);
    depth[s.id] = d;
    for (const child of delegates) if (child.id !== s.id && parentOf(child) === s.id) visit(child, d + 1);
  };
  visit(architect, 0);
  // Delegates on a cycle that never reaches the architect still show, under it.
  for (const s of delegates) if (!(s.id in depth)) visit(s, 1);
  return { key: "architect", sessions, depth };
}

/** The sessions' ids in the order the list shows them, for moving with J and K. */
export const displayOrder = (list) => group(list).flatMap((g) => g.sessions.map((s) => s.id));
