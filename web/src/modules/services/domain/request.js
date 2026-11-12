// Agents' service requests, as the Services page holds them (contract: Harness
// server ↔ Web UI — services, section 13). Pure functions over the module's
// request shape (infrastructure/dto.js).

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
