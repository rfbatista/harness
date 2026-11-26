import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { notification } from "../testing/fixtures";
import { toWire } from "../testing/wire";
import { NotificationDto } from "./dto/notification-dto";
import { toNotification } from "./notification-mapper";

describe("the session_memory_killed kind", () => {
  test("is read as itself, not as a generic agent notification", () => {
    const wire = toWire(notification({ kind: "session_memory_killed", level: "info", sessionId: "s1", title: "node jest ended", body: "3.5 GB of the 4 GB budget" }));
    const n = toNotification(NotificationDto.fromJSON(wire));
    assert.equal(n.kind, "session_memory_killed");
    assert.equal(n.level, "info");
    assert.equal(n.sessionId, "s1");
  });

  test("a kind this client does not know still reads as an agent notification", () => {
    const n = toNotification(NotificationDto.fromJSON({ ...toWire(notification()), kind: "something_new" }));
    assert.equal(n.kind, "agent");
  });
});
