// A call entry as the Live view's markup binds it: its collapsed row and the
// four tabs it expands into. Pure.

import { estimatedCost, tokens } from "../domain/format.js";
import { isInFlight } from "../domain/liveCalls.js";
import { reassembleMessage } from "../domain/message.js";

const enc = encodeURIComponent;
const pad = (n) => String(n).padStart(2, "0");
const clockTime = (d) => `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
const taskHref = (projectId, taskId) => (taskId ? `/projects/${enc(projectId)}/tasks/${enc(taskId)}` : "");

/** Calls with no project are the harness's own; the row's project is the page's. */
const sessionLabel = (e) => {
  if (e.session) return e.session.title || `Session ${e.session.id.slice(0, 8)}`;
  return e.kind === "document-assistant" ? "Document assistant" : "Harness";
};

/** @param {import("../domain/liveCalls.js").CallEntry} e @param {string} projectId */
export function callRow(e, projectId) {
  const inFlight = isInFlight(e);
  const usage = e.usage;
  const total = usage ? usage.input_tokens + usage.output_tokens + usage.cache_creation_input_tokens + usage.cache_read_input_tokens : 0;
  const taskLink = taskHref(projectId, e.task?.id);
  let status = "";
  if (e.noLongerInFlight) status = "no response";
  else if (e.finished) status = e.error || (e.status === 0 ? "no response" : String(e.status));
  return {
    id: e.id,
    time: clockTime(e.startedAt),
    iso: e.startedAt.toISOString(),
    session: sessionLabel(e),
    sessionHref: e.session && taskLink ? `${taskLink}?session=${enc(e.session.id)}` : "",
    task: e.task ? e.task.title || "Untitled task" : "No task",
    taskHref: taskLink,
    model: e.model,
    method: e.request.method,
    path: e.request.path,
    streamed: e.streamed,
    inFlight,
    status,
    failed: e.finished ? e.error !== "" || e.status === 0 || e.status >= 400 : false,
    tokens: usage ? tokens(total) : "",
    cost: usage ? estimatedCost(usage.estimated_cost_usd) : "",
    duration: e.finished ? `${(e.durationMs / 1000).toFixed(1)}s` : "",
  };
}

export const TABS = [
  { id: "request", label: "Request" },
  { id: "response", label: "Response" },
  { id: "message", label: "Message" },
  { id: "usage", label: "Usage" },
];

const messages = new WeakMap();

/** The message a finished call answered with: the reassembled stream, or the JSON body. */
function messageOf(e) {
  const r = e.response;
  if (!r) return null;
  if (!r.events) return r.body ?? null;
  if (!messages.has(r)) messages.set(r, reassembleMessage(r.events));
  return messages.get(r);
}

/**
 * The entry's four tabs and the JSON value each shows; a value that is not
 * there yet (the call is in flight) is null.
 * @param {import("../domain/liveCalls.js").CallEntry} e
 * @returns {{id: string, label: string, value: unknown}[]}
 */
export function callTabs(e) {
  const values = { request: e.request, response: e.response, message: messageOf(e), usage: e.usage };
  return TABS.map((t) => ({ ...t, value: values[t.id] }));
}
