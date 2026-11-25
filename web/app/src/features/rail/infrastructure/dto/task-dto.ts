// A ticket as the server sends it (internal/domain/ticket.go), as much as the
// rail reads: in the seed, in GET /api/list_tickets and in a `ticket` change.
// Fields keep the wire names. Unknown fields are never looked at.

import { DtoError } from "../../../../shared/infrastructure/dto/dto-error";
import { lenient, object, oneOf, optionalCount, optionalString, string, type Wire } from "../../../../shared/infrastructure/dto/fields";
import { TASK_STATUSES } from "../../domain/task";

const TASK = "TaskDto";
const MERGE = "MergeStateDto";

export class MergeStateDto {
  constructor(
    readonly state: string,
    readonly merged: number,
    readonly total: number,
  ) {}

  static fromJSON(raw: unknown, at = ""): MergeStateDto {
    const from = object(MERGE, raw, at);
    return new MergeStateDto(string(MERGE, from, "state", at), optionalCount(MERGE, from, "merged", at) ?? 0, optionalCount(MERGE, from, "total", at) ?? 0);
  }

  toJSON(): Wire {
    return { state: this.state, merged: this.merged, total: this.total };
  }
}

export class TaskDto {
  constructor(
    readonly id: string,
    readonly project_id: string,
    readonly title: string,
    readonly status: (typeof TASK_STATUSES)[number],
    readonly parent_id: string,
    readonly pending_reviews: number,
    readonly created_at: string,
    readonly updated_at: string,
    readonly status_since: string,
    readonly archived_at: string,
    readonly merge_state: MergeStateDto | undefined,
  ) {}

  static fromJSON(raw: unknown, at = ""): TaskDto {
    const from = object(TASK, raw, at);
    const id = string(TASK, from, "id", at);
    if (id === "") throw new DtoError(TASK, at ? `${at}.id` : "id", "expected a task id");
    return new TaskDto(
      id,
      optionalString(TASK, from, "project_id", at) ?? "",
      optionalString(TASK, from, "title", at) ?? "",
      oneOf(TASK, from, "status", TASK_STATUSES, at),
      optionalString(TASK, from, "parent_id", at) ?? "",
      optionalCount(TASK, from, "pending_reviews", at) ?? 0,
      optionalString(TASK, from, "created_at", at) ?? "",
      optionalString(TASK, from, "updated_at", at) ?? "",
      optionalString(TASK, from, "status_since", at) ?? "",
      optionalString(TASK, from, "archived_at", at) ?? "",
      // A merge state that does not read is left out rather than failing the task.
      from["merge_state"] === undefined || from["merge_state"] === null ? undefined : lenient(() => MergeStateDto.fromJSON(from["merge_state"], at ? `${at}.merge_state` : "merge_state")),
    );
  }

  toJSON(): Wire {
    return {
      id: this.id,
      project_id: this.project_id,
      title: this.title,
      status: this.status,
      parent_id: this.parent_id,
      pending_reviews: this.pending_reviews,
      created_at: this.created_at,
      updated_at: this.updated_at,
      status_since: this.status_since,
      archived_at: this.archived_at,
      ...(this.merge_state ? { merge_state: this.merge_state.toJSON() } : {}),
    };
  }
}
