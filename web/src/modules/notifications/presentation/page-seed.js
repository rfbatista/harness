// The notifications the server painted into the page: the top bar island's
// props carry them as `notifications.page` (the API's page), the same JSON the
// store's gateway reads as a seed.

const PROPS = 'script[data-island-props="top-bar"]';

/** The server's first paint, or null when the page has none. */
export function pageSeed(doc) {
  const el = doc.querySelector(PROPS);
  if (!el) return null;
  try {
    return JSON.parse(el.textContent ?? "null")?.notifications?.page ?? null;
  } catch {
    return null;
  }
}
