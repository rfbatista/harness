// The notifications the server painted into the page: the top bar island's
// props carry them as `notifications` (null when the inbox is not served) with
// `page` as the API's page, the same JSON the store's gateway reads as a seed.

const PROPS = 'script[data-island-props="top-bar"]';

function notificationsProp(doc) {
  const el = doc.querySelector(PROPS);
  if (!el) return null;
  try {
    return JSON.parse(el.textContent ?? "null")?.notifications ?? null;
  } catch {
    return null;
  }
}

/** Whether the server serves the inbox: its props carry a non-null `notifications`. */
export function inboxServed(doc) {
  return notificationsProp(doc) !== null;
}

/** The server's first paint, or null when the page has none. */
export function pageSeed(doc) {
  return notificationsProp(doc)?.page ?? null;
}
