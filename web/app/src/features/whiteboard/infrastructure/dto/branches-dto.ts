// A session's Branches list as GET /api/list_session_branches and the project
// feed's session_branches message send it. Only what a card says is read.

import { DtoError } from "../../../../shared/infrastructure/dto/dto-error";
import { array, object, optionalString, string, type Wire } from "../../../../shared/infrastructure/dto/fields";

const BRANCH = "BoardBranchDto";
const LIST = "SessionBranchesDto";

export class BoardBranchDto {
  constructor(
    readonly name: string,
    readonly is_session_branch: boolean,
    readonly state: string | undefined,
    readonly pushed: unknown[],
  ) {}

  static fromJSON(raw: unknown, at = ""): BoardBranchDto {
    const from = object(BRANCH, raw, at);
    const pushed = from.pushed ?? [];
    if (!Array.isArray(pushed)) throw new DtoError(BRANCH, at === "" ? "pushed" : `${at}.pushed`, "expected an array");
    return new BoardBranchDto(string(BRANCH, from, "name", at), from.is_session_branch === true, optionalString(BRANCH, from, "state", at), pushed);
  }

  toJSON(): Wire {
    return { name: this.name, is_session_branch: this.is_session_branch, state: this.state, pushed: this.pushed };
  }
}

export class SessionBranchesDto {
  constructor(
    readonly session_id: string,
    readonly branches: BoardBranchDto[],
  ) {}

  static fromJSON(raw: unknown, at = ""): SessionBranchesDto {
    const from = object(LIST, raw, at);
    return new SessionBranchesDto(string(LIST, from, "session_id", at), from.branches == null ? [] : array(LIST, from, "branches", (item, path) => BoardBranchDto.fromJSON(item, path), at));
  }

  toJSON(): Wire {
    return { session_id: this.session_id, branches: this.branches.map((b) => b.toJSON()) };
  }
}

/** GET /api/list_session_branches → { sessions: [...] }. */
export class TaskBranchesDto {
  constructor(readonly sessions: SessionBranchesDto[]) {}

  static fromJSON(raw: unknown): TaskBranchesDto {
    const from = object("TaskBranchesDto", raw);
    return new TaskBranchesDto(array("TaskBranchesDto", from, "sessions", (item, at) => SessionBranchesDto.fromJSON(item, at)));
  }

  toJSON(): Wire {
    return { sessions: this.sessions.map((s) => s.toJSON()) };
  }
}
