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

export interface ServerRow {
  readonly name: string;
  /** "Built in", "Token saver", "Agent", "Project" or "Mode". */
  readonly attached: string;
  /** Why it was attached, one line. */
  readonly attachedReason: string;
  readonly status: "connected" | "failed" | "unknown";
  /** "connected", "failed" or "status unknown". */
  readonly statusLabel: string;
  /** Claude's error for a failed server; for an unknown one, why the harness does not know. */
  readonly reason: string;
  /** The row is a failure: the only place the tab uses the danger tone. */
  readonly failed: boolean;
  /** Copy is offered, for a failed server's error. */
  readonly canCopy: boolean;
  readonly copied: boolean;
}

export interface ServersGroup extends GroupHeading {
  /** The count is in the danger tone: some server failed. */
  readonly danger: boolean;
  readonly rows: readonly ServerRow[];
}

export interface ContextGroups {
  readonly instructions: InstructionsGroup;
  readonly tools: ToolsGroup;
  readonly servers: ServersGroup;
}
