// What the Context tab's view renders. Every group has a count on its heading.

export type GroupKey = "instructions" | "tools" | "servers" | "skills" | "hooks" | "changes";

/** What a group's builder is given: the group's own identity and whether it is open. */
export interface HeadingBase {
  readonly key: GroupKey;
  readonly title: string;
  readonly open: boolean;
}

export interface GroupHeading extends HeadingBase {
  /** The count on the heading: "5", or "2 of 5" while a search filters the rows. */
  readonly count: string;
  /** A search is on and nothing in the group matches it. */
  readonly noMatch: boolean;
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
  /** The search matched only inside the piece's text. */
  readonly matchedInText: boolean;
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
  /** Changed just now: the view rings the row in place. */
  readonly ringing: boolean;
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
  readonly ringing: boolean;
}

export interface ServersGroup extends GroupHeading {
  /** The count is in the danger tone: some server failed. */
  readonly danger: boolean;
  readonly rows: readonly ServerRow[];
}

export interface SkillRow {
  readonly name: string;
  /** "Mode", "Repository", "Agent" or "Built in". */
  readonly source: string;
  readonly description: string;
  /** "invoked ×2", or "" when the session has not invoked it. */
  readonly invoked: string;
  readonly ringing: boolean;
}

export interface SkillsGroup extends GroupHeading {
  readonly rows: readonly SkillRow[];
}

export interface HookRow {
  readonly event: string;
  readonly matcher: string;
  readonly rule: string;
}

export interface HooksGroup extends GroupHeading {
  readonly rows: readonly HookRow[];
}

export interface ChangeRow {
  /** "21:05". */
  readonly time: string;
  /** "Skill beta invoked". */
  readonly text: string;
  readonly note: string;
}

export interface ChangesGroup extends GroupHeading {
  readonly rows: readonly ChangeRow[];
}

export interface SearchViewModel {
  readonly query: string;
  /** Rises each time the person asks for the search box ("/"): the view focuses it on a change. */
  readonly focusRequest: number;
}

export interface ContextGroups {
  readonly instructions: InstructionsGroup;
  readonly tools: ToolsGroup;
  readonly servers: ServersGroup;
  readonly skills: SkillsGroup;
  readonly hooks: HooksGroup;
  readonly changes: ChangesGroup;
}
