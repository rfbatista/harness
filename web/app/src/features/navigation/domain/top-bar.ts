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
