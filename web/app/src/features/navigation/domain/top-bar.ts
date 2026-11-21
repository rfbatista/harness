// The top bar's navigation model: the projects, the current one, and the links
// to the app-wide pages the server serves. The server builds it for a page
// path (the island props, and GET /api/top_bar?path=); the client never builds
// an href.

export const LINK_KEYS = ["board", "projects", "new_project", "agents", "credentials", "token_savers", "models", "agent_isolation", "server"] as const;

export type LinkKey = (typeof LINK_KEYS)[number];

export interface Project {
  readonly id: string;
  readonly name: string;
}

export interface NavLink {
  readonly key: LinkKey;
  readonly href: string;
  readonly current: boolean;
  /** Something there needs the person: today, Agent isolation blocks agents from starting. */
  readonly attention: boolean;
}

export interface TopBar {
  readonly path: string;
  readonly projects: readonly Project[];
  /** "" on a page that belongs to no project. */
  readonly currentProjectId: string;
  readonly switchProjectHref: string;
  readonly links: readonly NavLink[];
}

export type IconName = "key" | "saver" | "model" | "shield" | "server";

export interface LinkPresentation {
  readonly label: string;
  /** Icon links carry their label as an accessible name and a tooltip. */
  readonly icon?: IconName;
  /** A note that describes the link, shown as the tooltip and to screen readers. */
  readonly note?: string;
}

/** Labels, icons and notes are the client's, by key. */
export const LINK_PRESENTATION: Readonly<Record<LinkKey, LinkPresentation>> = {
  board: { label: "Agent board" },
  projects: { label: "Projects" },
  new_project: { label: "New project" },
  agents: { label: "Agents", note: "Agents are shared by every project" },
  credentials: { label: "Git credentials", icon: "key" },
  token_savers: { label: "Token savers", icon: "saver" },
  models: { label: "Models", icon: "model" },
  agent_isolation: { label: "Agent isolation", icon: "shield" },
  server: { label: "Server", icon: "server" },
};

export class TopBarError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "TopBarError";
  }
}

const isRecord = (value: unknown): value is Record<string, unknown> => typeof value === "object" && value !== null && !Array.isArray(value);

const isLinkKey = (key: unknown): key is LinkKey => (LINK_KEYS as readonly unknown[]).includes(key);

/** Reads the top-bar model from the server's JSON. Unknown fields and unknown link keys are ignored. */
export function parseTopBar(raw: unknown): TopBar {
  if (!isRecord(raw)) throw new TopBarError("the top-bar model is not an object");
  const { path, projects, current_project_id, switch_project_href, links } = raw;
  if (typeof path !== "string") throw new TopBarError("the top-bar model has no path");
  if (typeof switch_project_href !== "string") throw new TopBarError("the top-bar model has no switch_project_href");
  if (!Array.isArray(projects) || !Array.isArray(links)) throw new TopBarError("the top-bar model has no projects or links");
  return {
    path,
    projects: projects.map(toProject),
    currentProjectId: typeof current_project_id === "string" ? current_project_id : "",
    switchProjectHref: switch_project_href,
    links: links.filter((link) => isRecord(link) && isLinkKey(link.key)).map(toLink),
  };
}

function toProject(raw: unknown): Project {
  if (!isRecord(raw) || typeof raw.id !== "string" || typeof raw.name !== "string") throw new TopBarError("a project has no id or name");
  return { id: raw.id, name: raw.name };
}

function toLink(raw: unknown): NavLink {
  const link = raw as Record<string, unknown>;
  if (typeof link.href !== "string") throw new TopBarError(`link ${String(link.key)} has no href`);
  return { key: link.key as LinkKey, href: link.href, current: link.current === true, attention: link.attention === true };
}
