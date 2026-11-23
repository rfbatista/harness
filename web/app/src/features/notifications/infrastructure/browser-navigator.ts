import type { Navigator } from "../application/ports";

/** Opens a page with a full page load. */
export function browserNavigator(location: Pick<Location, "assign"> = globalThis.location): Navigator {
  return { open: (url) => location.assign(url) };
}
