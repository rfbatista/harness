// What the page event stream (GET /api/stream) sends, and what a tab asks of it
// (POST /api/stream/follow and /api/stream/unfollow).

import { boolean, object, string, type Wire } from "./fields";

const MESSAGE = "StreamMessageDto";
const FOLLOW = "FollowRequestDto";

/**
 * One stream message: the stream's first message names it (`stream`), then each
 * is the data of a followed `path`, or the end of that feed (`end`).
 */
export class StreamMessageDto {
  constructor(
    readonly stream: string | undefined,
    readonly path: string | undefined,
    readonly data: unknown,
    readonly end: boolean,
  ) {}

  static fromJSON(raw: unknown): StreamMessageDto {
    const from = object(MESSAGE, raw);
    if (from.stream !== undefined && from.stream !== "") return new StreamMessageDto(string(MESSAGE, from, "stream"), undefined, undefined, false);
    const path = string(MESSAGE, from, "path");
    const end = from.end === undefined ? false : boolean(MESSAGE, from, "end");
    return new StreamMessageDto(undefined, path, from.data, end);
  }

  toJSON(): Wire {
    return this.stream !== undefined ? { stream: this.stream } : { path: this.path, ...(this.end ? { end: true } : { data: this.data }) };
  }
}

/** The body of POST /api/stream/follow and /api/stream/unfollow. */
export class FollowRequestDto {
  constructor(
    readonly stream: string,
    readonly path: string,
  ) {}

  static fromJSON(raw: unknown): FollowRequestDto {
    const from = object(FOLLOW, raw);
    return new FollowRequestDto(string(FOLLOW, from, "stream"), string(FOLLOW, from, "path"));
  }

  toJSON(): Wire {
    return { stream: this.stream, path: this.path };
  }
}
