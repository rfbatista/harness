// The rail island's props (shell.RailProps): the rail seed's fields at the top
// (the project's tasks and sessions), and what else the rail draws. Fields keep
// the wire names.

import { DtoError } from "../shared/infrastructure/dto/dto-error";
import { array, boolean, count, object, optionalString, type Wire } from "../shared/infrastructure/dto/fields";
import { RailSeedDto } from "../features/rail/infrastructure/dto/rail-seed-dto";

const NAME = "RailPropsDto";
const NAV = "RailNavDto";

export class RailNavDto {
  constructor(
    readonly new_task_href: string,
    readonly board_href: string,
    readonly code_href: string,
    readonly services_href: string,
    readonly services_project_id: string,
    readonly services_running: number,
    readonly reviews_href: string,
    readonly documents_href: string,
    readonly design_href: string,
    readonly design_system_href: string,
    readonly design_system_proposals: number,
    readonly usage_href: string,
    readonly product_owner_href: string,
  ) {}

  static fromJSON(raw: unknown, at: string): RailNavDto {
    const from = object(NAV, raw, at);
    const href = (key: string): string => optionalString(NAV, from, key, at) ?? "";
    return new RailNavDto(
      href("new_task_href"),
      href("board_href"),
      href("code_href"),
      href("services_href"),
      href("services_project_id"),
      count(NAV, from, "services_running", at),
      href("reviews_href"),
      href("documents_href"),
      href("design_href"),
      href("design_system_href"),
      count(NAV, from, "design_system_proposals", at),
      href("usage_href"),
      href("product_owner_href"),
    );
  }

  toJSON(): Wire {
    return {
      new_task_href: this.new_task_href,
      board_href: this.board_href,
      code_href: this.code_href,
      services_href: this.services_href,
      services_project_id: this.services_project_id,
      services_running: this.services_running,
      reviews_href: this.reviews_href,
      documents_href: this.documents_href,
      design_href: this.design_href,
      design_system_href: this.design_system_href,
      design_system_proposals: this.design_system_proposals,
      usage_href: this.usage_href,
      product_owner_href: this.product_owner_href,
    };
  }
}

export class RailPropsDto {
  constructor(
    readonly seed: RailSeedDto,
    readonly project_name: string,
    readonly current_task: string,
    readonly page: string,
    readonly collapsed: readonly string[],
    readonly reports_feed: boolean,
    readonly nav: RailNavDto,
  ) {}

  static fromJSON(raw: unknown): RailPropsDto {
    const from = object(NAME, raw);
    return new RailPropsDto(
      RailSeedDto.fromJSON(raw),
      optionalString(NAME, from, "project_name") ?? "",
      optionalString(NAME, from, "current_task") ?? "",
      optionalString(NAME, from, "page") ?? "",
      array(NAME, from, "collapsed", (item, at) => {
        if (typeof item !== "string") throw new DtoError(NAME, at, "expected a string");
        return item;
      }),
      boolean(NAME, from, "reports_feed"),
      RailNavDto.fromJSON(from["nav"], "nav"),
    );
  }

  toJSON(): Wire {
    return { ...this.seed.toJSON(), project_name: this.project_name, current_task: this.current_task, page: this.page, collapsed: this.collapsed, reports_feed: this.reports_feed, nav: this.nav.toJSON() };
  }
}

/** The Tasks button's island props (shell.TasksToggleProps): what the server counted as needing the person. */
export class TasksToggleDto {
  constructor(readonly needs_you: number) {}

  static fromJSON(raw: unknown): TasksToggleDto {
    return new TasksToggleDto(count("TasksToggleDto", object("TasksToggleDto", raw), "needs_you"));
  }

  toJSON(): Wire {
    return { needs_you: this.needs_you };
  }
}
