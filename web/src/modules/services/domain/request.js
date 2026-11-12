// Agents' service requests, as the Services page holds them (contract: Harness
// server ↔ Web UI — services, section 13). Pure functions over the module's
// request shape (infrastructure/dto.js).

import { MASK } from "./mask.js";
import { agoLabel } from "./service.js";

export const isPending = (request) => request.state === "pending";

export const pendingCount = (requests) => requests.filter(isPending).length;

/** The list with `request` in it (replacing the one with its id), newest first. */
export function upsertRequest(requests, request) {
  return [...requests.filter((r) => r.id !== request.id), request].sort((a, b) => (a.requestedAt < b.requestedAt ? 1 : a.requestedAt > b.requestedAt ? -1 : 0));
}

/**
 * The request without the variable values: what is kept while its card is not open. The values come
 * from the list call when the person opens the requests, and are never kept from the feed.
 */
export function stripValues(request) {
  const proposed = request.proposed ? Object.fromEntries(Object.entries(request.proposed).filter(([key]) => key !== "env")) : null;
  return { ...request, env: request.env.map((e) => ({ ...e, value: "" })), proposed };
}

/** "created by go-developer" for a service an agent made; "" for the person's. */
export function createdByLabel(service) {
  if (service.createdBy.kind !== "session") return "";
  return `created by ${service.createdBy.agent || "an agent"}`;
}

/** "3 requests" waiting on a service; "" for none. */
export const pendingLabel = (n) => (n <= 0 ? "" : `${n} request${n === 1 ? "" : "s"}`);

/** The toolbar badge's accessible name. */
export const requestsLabel = (n) => `Requests, ${n} waiting on you`;

const VERBS = { create: "Create", update: "Change", start: "Start", restart: "Restart", stop: "Stop" };

const FIELD_LABELS = {
  "command.line": "Command line",
  "command.subdir": "Subfolder",
  "command.ports": "Ports",
  "image.ref": "Image",
  "image.args": "Arguments",
  "image.ports": "Image ports",
  repository_id: "Repository",
  export_repository_env: "Repository .env",
};

const ports = (list) => list.map((p) => (typeof p === "number" ? String(p) : `${p.host}:${p.container}/${p.protocol ?? "tcp"}`)).join(", ");

/** The definition a create request proposes, as label and value rows. */
function definitionRows(proposed) {
  const rows = [{ label: "Kind", value: proposed.kind }];
  if (proposed.command) {
    rows.push({ label: "Command line", value: proposed.command.line });
    if (proposed.command.subdir) rows.push({ label: "Subfolder", value: proposed.command.subdir });
    if ((proposed.command.ports ?? []).length > 0) rows.push({ label: "Ports", value: ports(proposed.command.ports) });
  }
  if (proposed.image) {
    rows.push({ label: "Image", value: proposed.image.ref });
    if ((proposed.image.args ?? []).length > 0) rows.push({ label: "Arguments", value: proposed.image.args.join(" ") });
    if ((proposed.image.ports ?? []).length > 0) rows.push({ label: "Image ports", value: ports(proposed.image.ports) });
  }
  if (proposed.export_repository_env) rows.push({ label: "Repository .env", value: "exported into the run" });
  return rows;
}

/** What came of an answered request; null while it is pending. */
function outcomeOf(request) {
  switch (request.state) {
    case "done": return { text: "Approved and done.", tone: "signal" };
    case "approved": return { text: "Approved. Doing it now…", tone: "signal" };
    case "failed": return { text: `Approved, but it failed: ${request.resultError}`, tone: "danger" };
    case "denied": return { text: request.note ? `Denied. Your note: ${request.note}` : "Denied.", tone: "" };
    case "expired": return { text: "Expired: its session ended before you answered.", tone: "attention" };
    default: return null;
  }
}

const WORKTREE_LINES = { main: "Runs in the main checkout.", own: "Runs in the agent's own worktree." };

function variableRow(v, action, revealed) {
  const open = revealed[v.name] === true;
  const hasValue = v.change === "set";
  const verb = open ? "Hide" : "Reveal";
  const changeText = v.change === "unset" ? "removed" : action === "start" || action === "restart" ? "set" : v.previousSet ? "changed" : "added";
  return {
    name: v.name,
    change: v.change,
    changeText,
    hasValue,
    masked: hasValue && !open,
    shown: hasValue ? (open ? v.value : MASK) : "",
    toggleText: verb,
    toggleName: `${verb} ${v.name}`,
  };
}

/**
 * What one request's card binds (contract section 13). `revealed` names the variables shown in clear. The
 * values come only from a request read with the list call: a request kept from the feed has none.
 * @param {import("./ports.js").ServiceRequest} request
 * @param {{ now: Date, projectId?: string, revealed?: Record<string, boolean> }} o
 */
export function requestCard(request, { now, projectId = "", revealed = {} }) {
  const session8 = request.sessionId.slice(0, 8);
  const sessionHref = request.taskId && projectId
    ? `/projects/${encodeURIComponent(projectId)}/tasks/${encodeURIComponent(request.taskId)}?session=${encodeURIComponent(request.sessionId)}`
    : "";
  const changes = request.changes.filter((c) => !c.field.startsWith("env.")).map((c) => ({ label: FIELD_LABELS[c.field] ?? c.field, before: c.before, after: c.after }));
  const definition = request.action === "create" && request.proposed ? definitionRows(request.proposed) : [];
  const variables = request.env.map((v) => variableRow(v, request.action, revealed));
  const withValue = variables.filter((v) => v.hasValue);
  const outcome = outcomeOf(request);
  const overrides = request.action === "start" || request.action === "restart";
  return {
    id: request.id,
    anchor: `service-request-${request.id}`,
    title: `${VERBS[request.action]} ${request.serviceName}`,
    summary: request.summary,
    pending: isPending(request),
    outcome,
    hasOutcome: outcome !== null,
    outcomeText: outcome?.text ?? "",
    outcomeTone: outcome?.tone || null,
    agentLine: `${request.agent || "an agent"} · session ${session8}`,
    age: agoLabel(request.requestedAt, now),
    sessionHref,
    hasSessionLink: sessionHref !== "",
    reason: request.reason,
    hasReason: request.reason !== "",
    definition,
    hasDefinition: definition.length > 0,
    changes,
    hasChanges: changes.length > 0,
    worktreeLine: WORKTREE_LINES[request.worktree] ?? "",
    hasWorktree: (WORKTREE_LINES[request.worktree] ?? "") !== "",
    variablesTitle: overrides ? "Run overrides" : "Variables",
    variables,
    hasVariables: variables.length > 0,
    hasRevealable: withValue.length > 0,
    allLabel: withValue.length > 0 && withValue.every((v) => !v.masked) ? "Hide all" : "Reveal all",
  };
}
