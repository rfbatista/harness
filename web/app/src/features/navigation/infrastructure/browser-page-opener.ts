import type { PageOpener } from "../application/ports";

/** Opens a page with a full page load, as the picker's GET form does. */
export function browserPageOpener(location: Pick<Location, "assign"> = globalThis.location): PageOpener {
  return { open: (url) => location.assign(url) };
}
