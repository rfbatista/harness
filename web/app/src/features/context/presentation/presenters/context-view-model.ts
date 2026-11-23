// What the Context tab's view renders. Every group has a count on its heading.

export type GroupKey = "instructions" | "tools" | "servers" | "skills" | "hooks" | "changes";

export interface GroupHeading {
  readonly key: GroupKey;
  readonly title: string;
  /** The count on the heading, e.g. "5". */
  readonly count: string;
  readonly open: boolean;
}

export type FullTextState = "idle" | "loading" | "failed" | "done";

export interface InstructionRow {
  readonly id: string;
  readonly title: string;
  readonly source: string;
  /** "" when the piece has no path or version: the view shows nothing. */
  readonly path: string;
  readonly version: string;
  /** "1.2 KB · ≈300 tokens". */
  readonly size: string;
  readonly open: boolean;
  /** The text on show: the first 4 KB, or all of it once fetched. */
  readonly text: string;
  /** "Show all" is offered: the text is cut and the rest is not loaded yet. */
  readonly canShowAll: boolean;
  readonly fullText: FullTextState;
}

export interface InstructionsGroup extends GroupHeading {
  readonly rows: readonly InstructionRow[];
}

export interface ContextGroups {
  readonly instructions: InstructionsGroup;
}
