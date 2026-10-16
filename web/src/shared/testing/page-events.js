// The page event stream's envelopes, as GET /api/page_events sends them, for
// the stub APIs that stand in for the server in the gateways' contract suites.

/** The first message of every page event stream. */
export const helloEnvelope = (streamId = "stream-1") => ({ kind: "hello", stream_id: streamId });

/** A project change: exactly what /api/events sent, under change. */
export const projectEnvelope = (change) => ({ kind: "project", change });

/** An inbox change: exactly what /api/notifications/events sent, beside kind. */
export const notificationsEnvelope = (payload) => ({ kind: "notifications", ...payload });

/** A session's event: exactly what /api/sessions/:id/events sent, under event. */
export const sessionEnvelope = (sessionId, event) => ({ kind: "session", session_id: sessionId, event });

/** The project a page event stream follows, from its URL ("" if it is not one). */
export function pageEventsProject(input) {
  const url = new URL(input, "http://harness.test");
  return url.pathname === "/api/page_events" ? (url.searchParams.get("project_id") ?? "") : null;
}
