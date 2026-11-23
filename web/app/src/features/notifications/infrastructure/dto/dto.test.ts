import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { DtoError } from "../../../../shared/infrastructure/dto/dto-error";
import { notification, read } from "../../testing/fixtures";
import { toWire } from "../../testing/wire";
import { BellPropsDto } from "./bell-props-dto";
import { NotificationDto } from "./notification-dto";
import { ChangeEventDto, NotificationPageDto, ReadAnswerDto } from "./notification-page-dto";
import { ReadRequestDto } from "./read-request-dto";

const wire = () => toWire(read(notification({ body: "b", interruptedSessions: [{ sessionId: "s1", projectId: "p1", taskId: "t1", taskTitle: "T", agentName: "go", link: "/s", state: "failed", reason: "r" }] })));
const failsAt = (dto: string, path: string) => (err: unknown) => err instanceof DtoError && err.dto === dto && err.path === path && err.message.includes(path);

describe("NotificationDto", () => {
  test("reads the contract's JSON with its wire names and round-trips through toJSON", () => {
    const json = wire();
    const dto = NotificationDto.fromJSON(json);
    assert.equal(dto.project_name, "harness");
    assert.equal(dto.interrupted_sessions[0]?.state, "failed");
    assert.deepEqual(JSON.parse(JSON.stringify(dto)), json);
  });

  test("ignores unknown fields", () => {
    const json = wire();
    assert.equal(NotificationDto.fromJSON({ ...json, future: { a: 1 } }).id, json.id);
  });

  test("an absent task, session, body, read time and resolved time are undefined or null", () => {
    const bare = toWire(notification({ taskId: "" }));
    const dto = NotificationDto.fromJSON(bare);
    assert.deepEqual([dto.task_id, dto.session_id, dto.body, dto.read_at, dto.resolved_at], [undefined, undefined, undefined, null, null]);
  });

  test("names the field for each kind of mismatch", () => {
    const bad = (change: Record<string, unknown>) => () => NotificationDto.fromJSON({ ...wire(), ...change });
    assert.throws(bad({ id: undefined }), failsAt("NotificationDto", "id"));
    assert.throws(bad({ id: "" }), failsAt("NotificationDto", "id"));
    assert.throws(bad({ kind: "mystery" }), failsAt("NotificationDto", "kind"));
    assert.throws(bad({ level: 3 }), failsAt("NotificationDto", "level"));
    assert.throws(bad({ source: "robot" }), failsAt("NotificationDto", "source"));
    assert.throws(bad({ link: "//evil.example" }), failsAt("NotificationDto", "link"));
    assert.throws(bad({ link: "x" }), failsAt("NotificationDto", "link"));
    assert.throws(bad({ created_at: "yesterday" }), failsAt("NotificationDto", "created_at"));
    assert.throws(bad({ created_at: undefined }), failsAt("NotificationDto", "created_at"));
    assert.throws(bad({ title: 5 }), failsAt("NotificationDto", "title"));
    assert.throws(bad({ interrupted_sessions: [{ session_id: "s", link: "/s", state: "lost" }] }), failsAt("InterruptedSessionDto", "interrupted_sessions[0].state"));
    assert.throws(bad({ interrupted_sessions: "none" }), failsAt("NotificationDto", "interrupted_sessions"));
  });

  test("refuses what is not an object", () => {
    for (const raw of [null, "n1", 4, []]) assert.throws(() => NotificationDto.fromJSON(raw), failsAt("NotificationDto", ""));
  });
});

describe("page, answer and change event DTOs", () => {
  test("read their shapes, ignore unknown fields and round-trip", () => {
    const n = wire();
    const page = { notifications: [n], unread_count: 3, next_before: "20" };
    assert.deepEqual(JSON.parse(JSON.stringify(NotificationPageDto.fromJSON({ ...page, extra: 1 }))), page);
    assert.deepEqual(JSON.parse(JSON.stringify(ReadAnswerDto.fromJSON({ notifications: [n], unread_count: 2 }))), { notifications: [n], unread_count: 2 });
    assert.deepEqual(JSON.parse(JSON.stringify(ChangeEventDto.fromJSON({ changed: [n], unread_count: 2 }))), { changed: [n], unread_count: 2 });
  });

  test("a page has no next_before at its end", () => {
    assert.equal(NotificationPageDto.fromJSON({ notifications: [], unread_count: 0 }).next_before, undefined);
  });

  test("name the path of a bad notification and of a bad count", () => {
    assert.throws(() => NotificationPageDto.fromJSON({ notifications: [wire(), { ...wire(), kind: "x" }], unread_count: 1 }), failsAt("NotificationDto", "notifications[1].kind"));
    assert.throws(() => NotificationPageDto.fromJSON({ notifications: [], unread_count: -1 }), failsAt("NotificationPageDto", "unread_count"));
    assert.throws(() => NotificationPageDto.fromJSON({ notifications: [], unread_count: 1.5 }), failsAt("NotificationPageDto", "unread_count"));
    assert.throws(() => ReadAnswerDto.fromJSON({ notifications: [] }), failsAt("ReadAnswerDto", "unread_count"));
    assert.throws(() => ChangeEventDto.fromJSON({ changed: {}, unread_count: 0 }), failsAt("ChangeEventDto", "changed"));
    assert.throws(() => ChangeEventDto.fromJSON("garbage"), failsAt("ChangeEventDto", ""));
  });
});

describe("ReadRequestDto", () => {
  test("goes out as ids or as all, and nothing else", () => {
    assert.equal(JSON.stringify(new ReadRequestDto(["a", "b"])), '{"ids":["a","b"]}');
    assert.equal(JSON.stringify(new ReadRequestDto(undefined, true)), '{"all":true}');
  });

  test("reads back what it sends, and refuses a bad ids or all", () => {
    assert.deepEqual(ReadRequestDto.fromJSON({ ids: ["a"] }).toJSON(), { ids: ["a"] });
    assert.throws(() => ReadRequestDto.fromJSON({ ids: [1] }), failsAt("ReadRequestDto", "ids"));
    assert.throws(() => ReadRequestDto.fromJSON({ all: false }), failsAt("ReadRequestDto", "all"));
  });
});

describe("BellPropsDto", () => {
  test("reads the inbox href and the page, or no page", () => {
    const withPage = BellPropsDto.fromJSON({ inbox_href: "/notifications", page: { notifications: [], unread_count: 3 }, more: 1 });
    assert.deepEqual([withPage.inbox_href, withPage.page?.unread_count], ["/notifications", 3]);
    assert.equal(BellPropsDto.fromJSON({ inbox_href: "/n", page: null }).page, null);
  });

  test("names the path inside the page", () => {
    assert.throws(() => BellPropsDto.fromJSON({ inbox_href: "/n", page: { notifications: [], unread_count: "3" } }), failsAt("NotificationPageDto", "page.unread_count"));
    assert.throws(() => BellPropsDto.fromJSON({ page: null }), failsAt("BellPropsDto", "inbox_href"));
  });
});
