// The bodies the web UI sends to the product owner's routes, and the short answers.

import { object, string, type Wire } from "../../../../shared/infrastructure/dto/fields";

const START = "StartThreadRequestDto";
const SEND = "SendMessageRequestDto";
const THREAD = "ThreadRequestDto";
const RENAME = "RenameThreadRequestDto";
const STARTED = "StartedDto";
const SENT = "SentDto";

/** POST /api/start_product_owner_thread. */
export class StartThreadRequestDto {
  constructor(
    readonly project_id: string,
    readonly message: string,
  ) {}

  static fromJSON(raw: unknown): StartThreadRequestDto {
    const from = object(START, raw);
    return new StartThreadRequestDto(string(START, from, "project_id"), string(START, from, "message"));
  }

  toJSON(): Wire {
    return { project_id: this.project_id, message: this.message };
  }
}

/** POST /api/send_product_owner_message. */
export class SendMessageRequestDto {
  constructor(
    readonly thread_id: string,
    readonly message: string,
  ) {}

  static fromJSON(raw: unknown): SendMessageRequestDto {
    const from = object(SEND, raw);
    return new SendMessageRequestDto(string(SEND, from, "thread_id"), string(SEND, from, "message"));
  }

  toJSON(): Wire {
    return { thread_id: this.thread_id, message: this.message };
  }
}

/** POST /api/stop_product_owner_turn. */
export class ThreadRequestDto {
  constructor(readonly thread_id: string) {}

  static fromJSON(raw: unknown): ThreadRequestDto {
    return new ThreadRequestDto(string(THREAD, object(THREAD, raw), "thread_id"));
  }

  toJSON(): Wire {
    return { thread_id: this.thread_id };
  }
}

/** POST /api/rename_product_owner_thread. */
export class RenameThreadRequestDto {
  constructor(
    readonly thread_id: string,
    readonly title: string,
  ) {}

  static fromJSON(raw: unknown): RenameThreadRequestDto {
    const from = object(RENAME, raw);
    return new RenameThreadRequestDto(string(RENAME, from, "thread_id"), string(RENAME, from, "title"));
  }

  toJSON(): Wire {
    return { thread_id: this.thread_id, title: this.title };
  }
}

export class StartedDto {
  constructor(
    readonly thread_id: string,
    readonly item_id: string,
  ) {}

  static fromJSON(raw: unknown): StartedDto {
    const from = object(STARTED, raw);
    return new StartedDto(string(STARTED, from, "thread_id"), string(STARTED, from, "item_id"));
  }

  toJSON(): Wire {
    return { thread_id: this.thread_id, item_id: this.item_id };
  }
}

export class SentDto {
  constructor(readonly item_id: string) {}

  static fromJSON(raw: unknown): SentDto {
    return new SentDto(string(SENT, object(SENT, raw), "item_id"));
  }

  toJSON(): Wire {
    return { item_id: this.item_id };
  }
}
