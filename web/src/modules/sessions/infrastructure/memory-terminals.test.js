import { flush } from "../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { memoryTerminals } from "./memory-terminals.js";

file("sessions/infrastructure/memory-terminals");

async function attach(options) {
  const terms = memoryTerminals(options);
  const snapshots = [];
  terms.gateway.attach("s1", { onSnapshot: (s) => snapshots.push(s), onOutput: () => {} });
  await flush();
  return snapshots[0];
}

test("the snapshot carries the history as scrollback, oldest line first", async () => {
  const snapshot = await attach({ greeting: "ready", history: ["$ make", "ok"] });
  assert.equal(snapshot.scrollback, "$ make\nok");
  assert.equal(snapshot.screen, "ready");
});

test("without history the snapshot's scrollback is empty, never missing", async () => {
  const snapshot = await attach({ greeting: "ready" });
  assert.equal(snapshot.scrollback, "");
});
