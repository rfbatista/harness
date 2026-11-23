// A message between tabs on the BroadcastChannel "harness-page-event-stream":
// the shared stream's protocol, which the Alpine client's shared-feed.js speaks
// too. The wire shape is theirs, unchanged.

import { DtoError } from "./dto-error";
import { object, oneOf, string, type Wire } from "./fields";

const NAME = "ChannelMessageDto";

export const CHANNEL_MESSAGE_TYPES = ["holder", "ping", "pong", "follow", "unfollow", "paths", "bye", "event", "status"] as const;
export type ChannelMessageType = (typeof CHANNEL_MESSAGE_TYPES)[number];
const STATUSES = ["connecting", "live", "paused", "resynced"] as const;

export class ChannelMessageDto {
  constructor(
    readonly type: ChannelMessageType,
    readonly from: string,
    readonly path: string | undefined = undefined,
    readonly paths: readonly string[] | undefined = undefined,
    readonly data: unknown = undefined,
    readonly status: (typeof STATUSES)[number] | undefined = undefined,
    readonly to: string | undefined = undefined,
    readonly delay: number | undefined = undefined,
  ) {}

  static fromJSON(raw: unknown): ChannelMessageDto {
    const from = object(NAME, raw);
    const type = oneOf(NAME, from, "type", CHANNEL_MESSAGE_TYPES);
    const sender = string(NAME, from, "from");
    const needsPath = type === "follow" || type === "unfollow" || type === "event" || type === "status";
    const path = needsPath ? string(NAME, from, "path") : undefined;
    let paths: string[] | undefined;
    if (type === "paths") {
      if (!Array.isArray(from.paths) || from.paths.some((p) => typeof p !== "string")) throw new DtoError(NAME, "paths", "expected an array of strings");
      paths = from.paths as string[];
    }
    const status = type === "status" ? oneOf(NAME, from, "status", STATUSES) : undefined;
    const to = from.to === undefined ? undefined : string(NAME, from, "to");
    let delay: number | undefined;
    if (type === "holder" && from.delay !== undefined) {
      if (typeof from.delay !== "number" || !Number.isFinite(from.delay) || from.delay < 0) throw new DtoError(NAME, "delay", "expected a delay in milliseconds");
      delay = from.delay;
    }
    return new ChannelMessageDto(type, sender, path, paths, type === "event" ? from.data : undefined, status, to, delay);
  }

  toJSON(): Wire {
    return {
      type: this.type,
      from: this.from,
      ...(this.path === undefined ? {} : { path: this.path }),
      ...(this.paths === undefined ? {} : { paths: this.paths }),
      ...(this.type === "event" ? { data: this.data } : {}),
      ...(this.status === undefined ? {} : { status: this.status }),
      ...(this.to === undefined ? {} : { to: this.to }),
      ...(this.delay === undefined ? {} : { delay: this.delay }),
    };
  }
}
