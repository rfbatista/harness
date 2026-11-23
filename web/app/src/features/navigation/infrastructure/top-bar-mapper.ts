// The top-bar DTO as domain values. A link whose key the client does not know
// is dropped (the server may add pages before the client has a label for them).

import { LINK_KEYS, type LinkKey, type NavLink, type TopBar } from "../domain/top-bar";
import type { TopBarDto } from "./dto/top-bar-dto";

const isLinkKey = (key: string): key is LinkKey => (LINK_KEYS as readonly string[]).includes(key);

export function toTopBar(dto: TopBarDto): TopBar {
  const links: NavLink[] = [];
  for (const link of dto.links) if (isLinkKey(link.key)) links.push({ key: link.key, href: link.href, current: link.current, attention: link.attention });
  return {
    path: dto.path,
    projects: dto.projects.map(({ id, name }) => ({ id, name })),
    currentProjectId: dto.current_project_id,
    switchProjectHref: dto.switch_project_href,
    links,
  };
}
