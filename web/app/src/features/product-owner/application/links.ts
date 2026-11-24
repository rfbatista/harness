// The pages of the product owner, as the harness serves them.

const project = (projectId: string): string => `/projects/${encodeURIComponent(projectId)}`;

/** The full page, with its thread list. */
export const pageHref = (projectId: string): string => `${project(projectId)}/product-owner`;

/** A thread's own URL: it can be reopened from the address bar and shared. */
export const threadHref = (projectId: string, threadId: string): string => `${pageHref(projectId)}/threads/${encodeURIComponent(threadId)}`;

/** The project's Settings page at its product owner section. */
export const settingsHref = (projectId: string): string => `${project(projectId)}/settings#product-owner`;

/** The Context tab of the thread's session: the agent, prompt and model the thread started with (ADR 0099). */
export const contextHref = (projectId: string, sessionId: string): string => `${project(projectId)}/sessions/${encodeURIComponent(sessionId)}?tab=context`;
