// The top-bar model as the server sends it (Contract: Harness server ↔ Web UI —
// the top bar island, §1): GET /api/top_bar?path= answers it, and the island
// props carry it. Fields keep the wire names. The model's `notifications` part
// belongs to the notifications feature and is not read here.

import { array, boolean, object, string, type Wire } from "../../../../shared/infrastructure/dto/fields";

const PROJECT = "ProjectDto";
const LINK = "LinkDto";
const TOP_BAR = "TopBarDto";

export class ProjectDto {
  constructor(
    readonly id: string,
    readonly name: string,
  ) {}

  static fromJSON(raw: unknown, at = ""): ProjectDto {
    const from = object(PROJECT, raw, at);
    return new ProjectDto(string(PROJECT, from, "id", at), string(PROJECT, from, "name", at));
  }

  toJSON(): Wire {
    return { id: this.id, name: this.name };
  }
}

export class LinkDto {
  constructor(
    readonly key: string,
    readonly href: string,
    readonly current: boolean,
    readonly attention: boolean,
  ) {}

  static fromJSON(raw: unknown, at = ""): LinkDto {
    const from = object(LINK, raw, at);
    return new LinkDto(string(LINK, from, "key", at), string(LINK, from, "href", at), boolean(LINK, from, "current", at), boolean(LINK, from, "attention", at));
  }

  toJSON(): Wire {
    return { key: this.key, href: this.href, current: this.current, attention: this.attention };
  }
}

export class TopBarDto {
  constructor(
    readonly path: string,
    readonly projects: readonly ProjectDto[],
    readonly current_project_id: string,
    readonly switch_project_href: string,
    readonly links: readonly LinkDto[],
  ) {}

  static fromJSON(raw: unknown): TopBarDto {
    const from = object(TOP_BAR, raw);
    return new TopBarDto(
      string(TOP_BAR, from, "path"),
      array(TOP_BAR, from, "projects", (item, at) => ProjectDto.fromJSON(item, at)),
      string(TOP_BAR, from, "current_project_id"),
      string(TOP_BAR, from, "switch_project_href"),
      array(TOP_BAR, from, "links", (item, at) => LinkDto.fromJSON(item, at)),
    );
  }

  toJSON(): Wire {
    return {
      path: this.path,
      projects: this.projects.map((p) => p.toJSON()),
      current_project_id: this.current_project_id,
      switch_project_href: this.switch_project_href,
      links: this.links.map((l) => l.toJSON()),
    };
  }
}
