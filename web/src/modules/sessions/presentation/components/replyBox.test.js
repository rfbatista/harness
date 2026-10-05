import { Codes } from "../../../../shared/domain/errors.js";
import { mount } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryGateway } from "../../infrastructure/memory-gateway.js";
import { makeSession } from "../../testing/fixtures.js";
import { replyBox } from "./replyBox.js";

file("sessions/presentation/replyBox");

function setup() {
  const memory = memoryGateway({
    sessions: [makeSession({ id: "turn", status: "idle" }), makeSession({ id: "over", status: "done" })],
  });
  const mounted = mount(replyBox({ gateway: memory.gateway }));
  mounted.instance.init();
  return { ...mounted, memory };
}

test("sends the trimmed draft, clears it, and reports reply-sent", async () => {
  const { instance, set, memory, dispatched } = setup();
  assert.equal(instance.cannotType, true, "nothing to reply to yet");
  set("sessionId", "turn");
  instance.draft = "   ";
  assert.equal(instance.cannotSend, true, "blank drafts are not sent");
  instance.draft = "  run make check  ";
  await instance.send();
  assert.deepEqual(memory.sent.get("turn"), ["run make check"]);
  assert.equal(instance.draft, "");
  assert.deepEqual(dispatched, [{ name: "reply-sent", detail: { sessionId: "turn" } }]);
});

test("a finished session closes the box until another session is chosen", async () => {
  const { instance, set } = setup();
  set("sessionId", "over");
  instance.draft = "hello?";
  await instance.send();
  assert.equal(instance.error.code, Codes.SESSION_NOT_RUNNING);
  assert.equal(instance.cannotType, true);
  set("sessionId", "turn");
  assert.equal(instance.cannotType, false);
  assert.equal(instance.error, null);
});
