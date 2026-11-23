import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { DtoError } from "../shared/infrastructure/dto/dto-error";
import { topBarJson } from "../features/navigation/testing/fixtures";
import { TopBarPropsDto } from "./top-bar-props-dto";

const withBell = (notifications: unknown) => topBarJson({ notifications });

describe("TopBarPropsDto", () => {
  test("composes the navigation DTO and the bell DTO, and round-trips", () => {
    const json = withBell({ inbox_href: "/notifications", page: { notifications: [], unread_count: 3 } });
    const dto = TopBarPropsDto.fromJSON({ ...json, future: 1 });
    assert.equal(dto.topBar.projects[0]?.name, "harness");
    assert.equal(dto.notifications?.page?.unread_count, 3);
    assert.deepEqual(JSON.parse(JSON.stringify(dto)), { ...json, notifications: { inbox_href: "/notifications", page: { notifications: [], unread_count: 3 } } });
  });

  test("an inbox that is not served is no bell", () => {
    assert.equal(TopBarPropsDto.fromJSON(withBell(null)).notifications, null);
    assert.equal(TopBarPropsDto.fromJSON({ ...topBarJson(), notifications: undefined }).notifications, null);
  });

  test("a bell part that cannot be read is no bell", () => {
    for (const part of ["x", {}, { inbox_href: 3 }]) assert.equal(TopBarPropsDto.fromJSON(withBell(part)).notifications, null);
  });

  test("a bell page that is null or cannot be read leaves the bell, with no page", () => {
    for (const page of [null, { notifications: [], unread_count: "3" }, { notifications: [{ id: "" }], unread_count: 1 }]) {
      const bell = TopBarPropsDto.fromJSON(withBell({ inbox_href: "/n", page })).notifications;
      assert.deepEqual([bell?.inbox_href, bell?.page], ["/n", null]);
    }
  });

  test("a navigation part that cannot be read throws a DtoError naming the field", () => {
    assert.throws(() => TopBarPropsDto.fromJSON(topBarJson({ links: "none" as never })), (err: unknown) => err instanceof DtoError && err.path === "links");
    for (const raw of [null, "x", 3, []]) assert.throws(() => TopBarPropsDto.fromJSON(raw), DtoError);
  });
});
