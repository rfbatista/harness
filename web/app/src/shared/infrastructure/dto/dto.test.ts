import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { ApiErrorDto } from "./api-error-dto";
import { ChannelMessageDto } from "./channel-message-dto";
import { DtoError } from "./dto-error";
import { FollowRequestDto, StreamMessageDto } from "./stream-dto";

const failsAt = (dto: string, path: string) => (err: unknown) => err instanceof DtoError && err.dto === dto && err.path === path && err.message.includes(dto);
const roundTrips = (dto: { toJSON(): unknown }, json: unknown) => assert.deepEqual(JSON.parse(JSON.stringify(dto)), json);

describe("StreamMessageDto", () => {
  test("reads the hello, a feed's data and a feed's end, ignoring unknown fields", () => {
    roundTrips(StreamMessageDto.fromJSON({ stream: "st1", x: 1 }), { stream: "st1" });
    roundTrips(StreamMessageDto.fromJSON({ path: "/events", data: { a: 1 }, x: 1 }), { path: "/events", data: { a: 1 } });
    roundTrips(StreamMessageDto.fromJSON({ path: "/events", end: true }), { path: "/events", end: true });
  });

  test("refuses a message with no path and no stream, a bad end, and what is not an object", () => {
    assert.throws(() => StreamMessageDto.fromJSON({ data: 1 }), failsAt("StreamMessageDto", "path"));
    assert.throws(() => StreamMessageDto.fromJSON({ path: "/e", end: "yes" }), failsAt("StreamMessageDto", "end"));
    for (const raw of [null, "x", 3, [1]]) assert.throws(() => StreamMessageDto.fromJSON(raw), failsAt("StreamMessageDto", ""));
  });
});

describe("FollowRequestDto", () => {
  test("goes out as {stream, path} and reads back", () => {
    roundTrips(new FollowRequestDto("st1", "/events"), { stream: "st1", path: "/events" });
    assert.equal(FollowRequestDto.fromJSON({ stream: "st1", path: "/e", more: 1 }).path, "/e");
    assert.throws(() => FollowRequestDto.fromJSON({ stream: 1, path: "/e" }), failsAt("FollowRequestDto", "stream"));
  });
});

describe("ApiErrorDto", () => {
  test("reads the API's error body", () => {
    roundTrips(ApiErrorDto.fromJSON({ error: "bad", code: "INVALID_INPUT", extra: 1 }), { error: "bad", code: "INVALID_INPUT" });
    assert.throws(() => ApiErrorDto.fromJSON({ error: "bad" }), failsAt("ApiErrorDto", "code"));
    assert.throws(() => ApiErrorDto.fromJSON("bad"), failsAt("ApiErrorDto", ""));
  });
});

describe("ChannelMessageDto", () => {
  const valid: [string, Record<string, unknown>][] = [
    ["holder", { type: "holder", from: "a", delay: 1000 }],
    ["ping", { type: "ping", from: "a" }],
    ["pong", { type: "pong", from: "a" }],
    ["follow", { type: "follow", from: "a", path: "/events" }],
    ["unfollow", { type: "unfollow", from: "a", path: "/events" }],
    ["paths", { type: "paths", from: "a", paths: ["/a", "/b"] }],
    ["bye", { type: "bye", from: "a" }],
    ["event", { type: "event", from: "a", path: "/events", data: { n: 1 } }],
    ["status", { type: "status", from: "a", path: "/events", status: "live", to: "b" }],
  ];
  for (const [type, json] of valid) {
    test(`a ${type} message round-trips and ignores unknown fields`, () => {
      roundTrips(ChannelMessageDto.fromJSON({ ...json, extra: 1 }), json);
    });
  }

  test("names the field of a bad message", () => {
    assert.throws(() => ChannelMessageDto.fromJSON({ type: "teleport", from: "a" }), failsAt("ChannelMessageDto", "type"));
    assert.throws(() => ChannelMessageDto.fromJSON({ type: "ping" }), failsAt("ChannelMessageDto", "from"));
    assert.throws(() => ChannelMessageDto.fromJSON({ type: "follow", from: "a" }), failsAt("ChannelMessageDto", "path"));
    assert.throws(() => ChannelMessageDto.fromJSON({ type: "paths", from: "a", paths: [1] }), failsAt("ChannelMessageDto", "paths"));
    assert.throws(() => ChannelMessageDto.fromJSON({ type: "status", from: "a", path: "/p", status: "sleeping" }), failsAt("ChannelMessageDto", "status"));
    assert.throws(() => ChannelMessageDto.fromJSON({ type: "holder", from: "a", delay: -1 }), failsAt("ChannelMessageDto", "delay"));
    for (const raw of [null, "ping", undefined]) assert.throws(() => ChannelMessageDto.fromJSON(raw), failsAt("ChannelMessageDto", ""));
  });
});
