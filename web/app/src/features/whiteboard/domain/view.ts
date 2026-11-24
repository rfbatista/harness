// The two views of a task's sessions (ADR 0108): the list is the default.

export const AGENTS_VIEWS = ["list", "whiteboard"] as const;
export type AgentsView = (typeof AGENTS_VIEWS)[number];
