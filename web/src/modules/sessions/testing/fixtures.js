// Sessions for tests and web/dev pages.

export const T0 = new Date("2026-10-02T14:00:00Z");

/** @returns {import("../domain/session.js").Session} */
export function makeSession(overrides = {}) {
  return Object.freeze({
    id: "s1",
    projectId: "p1",
    ticketId: "t1",
    repositoryId: "r1",
    task: "Port tickets screen to httpclient",
    agentId: "backend",
    mode: "",
    status: "running",
    pendingApprovals: 0,
    lastAction: "",
    interactive: true,
    runsOn: "server",
    runnerHost: "",
    resumable: false,
    resumeBlocked: "SESSION_ALREADY_RUNNING",
    parentSessionId: "",
    role: "",
    architectSessionId: "",
    statusCheck: null,
    branch: "agent/port-tickets-1a2b3c4d",
    workspaceId: "ws1",
    workingDir: "/w/harness/.worktrees/port-tickets",
    updatedAt: T0,
    ...overrides,
  });
}

/** The wire format of a session, as GET /api/sessions returns it. */
export function toDTO(session) {
  return {
    id: session.id,
    project_id: session.projectId,
    ticket_id: session.ticketId || undefined,
    repository_id: session.repositoryId || undefined,
    task: session.task,
    agent_id: session.agentId || undefined,
    mode: session.mode || undefined,
    status: session.status,
    pending_approvals: session.pendingApprovals,
    last_action: session.lastAction || undefined,
    interactive: session.interactive,
    runs_on: session.runsOn || undefined,
    runner_host: session.runnerHost || undefined,
    resumable: session.resumable === true,
    resume_blocked: session.resumeBlocked ?? "",
    parent_session_id: session.parentSessionId || undefined,
    role: session.role ?? "",
    architect_session_id: session.architectSessionId || null,
    status_check: session.statusCheck ? statusCheckDTO(session.statusCheck) : null,
    branch: session.branch || undefined,
    workspace_id: session.workspaceId || undefined,
    working_dir: session.workingDir,
    updated_at: session.updatedAt.toISOString(),
  };
}

/** The wire format of a status check. */
export function statusCheckDTO(check) {
  return {
    task_id: check.taskId,
    architect_session_id: check.architectSessionId,
    delegate_session_id: check.delegateSessionId,
    every_minutes: check.everyMinutes,
    next_at: check.nextAt?.toISOString(),
    last_fired_at: check.lastFiredAt?.toISOString(),
    fired_count: check.firedCount,
    state: check.state,
  };
}
