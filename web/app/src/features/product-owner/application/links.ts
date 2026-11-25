// The pages of the product owner, as the harness serves them.

const project = (projectId: string): string => `/projects/${encodeURIComponent(projectId)}`;

/** The full page, with its thread list. */
export const pageHref = (projectId: string): string => `${project(projectId)}/product-owner`;

/** A thread's own URL: it can be reopened from the address bar and shared. */
export const threadHref = (projectId: string, threadId: string): string => `${pageHref(projectId)}/threads/${encodeURIComponent(threadId)}`;

/** The project's Settings page at its product owner section. */
export const settingsHref = (projectId: string): string => `${project(projectId)}/settings#product-owner`;

/**
 * The Context tab of the thread's session (ADR 0099): where the agent, prompt and model a thread
 * started with are read. The tab is not served for a session that has no task, so there is no link
 * to give yet; this is the one place that will build it.
 */
export const contextHref = (): string | null => null;

/** The Memory tab of the full page, at an entry when one is named: a note in a thread links here. */
export const memoryHref = (projectId: string, entryId = ""): string => `${pageHref(projectId)}#memory${entryId === "" ? "" : `/${encodeURIComponent(entryId)}`}`;

/** `#memory` or `#memory/<entry>` of a page address: the Memory tab, and the entry it opens at. */
export function memoryAddress(hash: string): { readonly tab: "memory"; readonly entryId: string } | undefined {
  const match = /^#memory(?:\/(.+))?$/.exec(hash);
  return match ? { tab: "memory", entryId: match[1] === undefined ? "" : decodeURIComponent(match[1]) } : undefined;
}
