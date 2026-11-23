import type { TopBar } from "../domain/top-bar";
import { TopBarDto } from "../infrastructure/dto/top-bar-dto";
import { toTopBar } from "../infrastructure/top-bar-mapper";

// The top-bar model as the server sends it (the contract's JSON), for tests.

export interface TopBarJson {
  path: string;
  projects: { id: string; name: string }[];
  current_project_id: string;
  switch_project_href: string;
  links: { key: string; href: string; current: boolean; attention: boolean }[];
  notifications: unknown;
  [extra: string]: unknown;
}

export const SERVED_LINKS: TopBarJson["links"] = [
  { key: "board", href: "/", current: false, attention: false },
  { key: "projects", href: "/projects", current: false, attention: false },
  { key: "agents", href: "/agents", current: false, attention: false },
  { key: "credentials", href: "/credentials", current: false, attention: false },
  { key: "token_savers", href: "/token-savers", current: false, attention: false },
  { key: "models", href: "/models", current: false, attention: false },
  { key: "agent_isolation", href: "/agent-isolation", current: false, attention: false },
  { key: "server", href: "/server", current: false, attention: false },
];

export const PROJECTS = [
  { id: "p1", name: "harness" },
  { id: "p2", name: "Website" },
];

export function topBarJson(overrides: Partial<TopBarJson> = {}): TopBarJson {
  return {
    path: "/",
    projects: PROJECTS,
    current_project_id: "",
    switch_project_href: "/switch-project",
    links: SERVED_LINKS,
    notifications: null,
    ...overrides,
  };
}

/** The served links with the given key marked current and/or attention. */
export function linksWith(marks: Record<string, { current?: boolean; attention?: boolean }>): TopBarJson["links"] {
  return SERVED_LINKS.map((link) => ({ ...link, ...marks[link.key] }));
}

/** The domain top bar for the contract's JSON, as the HTTP adapter reads it. */
export function topBarFromJson(json: unknown): TopBar {
  return toTopBar(TopBarDto.fromJSON(json));
}
