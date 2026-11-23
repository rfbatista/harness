// The ledger fields of a notification (ADR 0096, ADR 0097): who caused it, what
// the verb acts on, and the sessions a commit_check_off item lists.

import { boolean, object, optionalString, string, type Wire } from "../../../../shared/infrastructure/dto/fields";

const ACTOR = "ActorDto";
const OBJECT = "ObjectDto";
const LISTED = "ListedSessionDto";

/** Who caused a notification: an agent, the system or the person. */
export class ActorDto {
  constructor(
    readonly kind: string,
    readonly name: string,
    readonly mode: string | undefined,
    readonly session_id: string | undefined,
  ) {}

  static fromJSON(raw: unknown, at = ""): ActorDto {
    const from = object(ACTOR, raw, at);
    return new ActorDto(string(ACTOR, from, "kind", at), string(ACTOR, from, "name", at), optionalString(ACTOR, from, "mode", at), optionalString(ACTOR, from, "session_id", at));
  }

  toJSON(): Wire {
    return { kind: this.kind, name: this.name, mode: this.mode, session_id: this.session_id };
  }
}

/** What the verb acts on; `code` shows it monospace. */
export class ObjectDto {
  constructor(
    readonly text: string,
    readonly code: boolean,
  ) {}

  static fromJSON(raw: unknown, at = ""): ObjectDto {
    const from = object(OBJECT, raw, at);
    return new ObjectDto(string(OBJECT, from, "text", at), from.code === undefined ? false : boolean(OBJECT, from, "code", at));
  }

  toJSON(): Wire {
    return { text: this.text, code: this.code };
  }
}

/** One session a commit_check_off item lists. */
export class ListedSessionDto {
  constructor(
    readonly session_id: string,
    readonly task_id: string | undefined,
    readonly task_title: string | undefined,
    readonly agent_name: string | undefined,
    readonly link: string,
    readonly state: string,
  ) {}

  static fromJSON(raw: unknown, at = ""): ListedSessionDto {
    const from = object(LISTED, raw, at);
    return new ListedSessionDto(
      string(LISTED, from, "session_id", at),
      optionalString(LISTED, from, "task_id", at),
      optionalString(LISTED, from, "task_title", at),
      optionalString(LISTED, from, "agent_name", at),
      string(LISTED, from, "link", at),
      string(LISTED, from, "state", at),
    );
  }

  toJSON(): Wire {
    return { session_id: this.session_id, task_id: this.task_id, task_title: this.task_title, agent_name: this.agent_name, link: this.link, state: this.state };
  }
}
