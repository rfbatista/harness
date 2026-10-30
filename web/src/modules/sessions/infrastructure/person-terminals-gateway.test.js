import { apiClient } from "../../../shared/infrastructure/api.js";
import { fakeFetch, jsonResponse } from "../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { personTerminalsGateway, toPersonTerminal } from "./person-terminals-gateway.js";

file("sessions/infrastructure/person-terminals-gateway");

const dto = { id: "pt_1", session_id: "s1", root: "worktree", dir: "web/src", label: "web/src", state: "running", opened_at: "2026-10-29T12:00:00Z" };

function setup(handler) {
  const http = fakeFetch(handler);
  const followed = [];
  const feed = { follow: (path, handlers) => (followed.push({ path, ...handlers }), () => followed.pop()) };
  return { gateway: personTerminalsGateway(apiClient({ base: "/api", fetch: http.fetch }), feed), http, followed };
}

test("a terminal reads as the contract's shape", () => {
  const t = toPersonTerminal(dto);
  assert.equal(t.id, "pt_1");
  assert.equal(t.sessionId, "s1");
  assert.equal(t.root, "worktree");
  assert.equal(t.dir, "web/src");
  assert.equal(t.label, "web/src");
  assert.equal(t.openedAt.toISOString(), "2026-10-29T12:00:00.000Z");
});

test("a terminal with no id is a bad response", () => {
  assert.throws(() => toPersonTerminal({ ...dto, id: "" }), "BAD_RESPONSE");
});

test("list reads the session's terminals, and open posts the root, the folder and the size", async () => {
  const { gateway, http } = setup((url, init) => (init.method === "POST" ? jsonResponse(201, dto) : jsonResponse(200, [dto])));
  assert.equal((await gateway.list("s1")).length, 1);
  assert.equal(http.calls[0].url, "/api/sessions/s1/person-terminals");

  const t = await gateway.open("s1", { root: "worktree", dir: "web/src", cols: 120, rows: 32 });
  assert.equal(t.id, "pt_1");
  assert.equal(http.calls[1].init.method, "POST");
  assert.deepEqual(JSON.parse(http.calls[1].init.body), { root: "worktree", dir: "web/src", cols: 120, rows: 32 });
});

test("a refused open carries the server's code and message", async () => {
  const { gateway } = setup(() => jsonResponse(403, { error: "No terminal can open from another site.", code: "TERMINAL_ORIGIN_REFUSED" }));
  const err = await assert.rejects(gateway.open("s1", { root: "worktree", dir: "", cols: 80, rows: 24 }), "TERMINAL_ORIGIN_REFUSED");
  assert.equal(err.message, "No terminal can open from another site.");
});

test("close deletes the terminal", async () => {
  const { gateway, http } = setup(() => new Response(null, { status: 204 }));
  await gateway.close("pt_1");
  assert.equal(http.calls[0].url, "/api/person-terminals/pt_1");
  assert.equal(http.calls[0].init.method, "DELETE");
});

test("the session feed's person_terminal_ended events are told, and nothing else is", () => {
  const { gateway, followed } = setup(() => jsonResponse(200, []));
  const ended = [];
  gateway.followEnded("s1", (e) => ended.push(e));
  assert.equal(followed[0].path, "/sessions/s1/events");
  followed[0].onMessage({ type: "status", session_id: "s1" });
  followed[0].onMessage({ type: "person_terminal_ended", session_id: "s1", id: "pt_1", reason: "root_gone", message: "The session ended, and its resources folder with it." });
  followed[0].onMessage({ type: "person_terminal_ended", session_id: "s1" });
  assert.deepEqual(ended, [{ id: "pt_1", sessionId: "s1", reason: "root_gone", message: "The session ended, and its resources folder with it." }]);
});
