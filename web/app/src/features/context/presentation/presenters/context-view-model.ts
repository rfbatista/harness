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

export interface ToolRow {
  /** The name on the row: an MCP tool without its "mcp__<server>__" prefix. */
  readonly name: string;
  /** The tool's full name, for the title and for search. */
  readonly fullName: string;
  readonly state: "allowed" | "on_demand" | "hidden" | "refused";
  /** "allowed", "on demand", "hidden" or "refused": text, never only colour. */
  readonly stateLabel: string;
  /** "used ×3", or "" when the session has not used it. */
  readonly used: string;
}

export interface ToolSection {
  /** "Claude Code", or the MCP server's name. */
  readonly title: string;
  readonly count: string;
  readonly rows: readonly ToolRow[];
}

export interface ToolsGroup extends GroupHeading {
  readonly sections: readonly ToolSection[];
}

export interface ContextGroups {
  readonly instructions: InstructionsGroup;
  readonly tools: ToolsGroup;
}
