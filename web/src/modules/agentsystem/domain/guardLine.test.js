import { assert, file, test } from "../../../shared/testing/test.js";
import { guardLine, loadingLine } from "./guardLine.js";

file("agentsystem/domain/guardLine");

const answer = (missing) => ({ effective: { missing_required_gates: missing } });

test("a repository with every required gate answers a neutral line", () => {
  assert.deepEqual(guardLine(answer(undefined)), { state: "ok", text: "Guard: every required gate has a command", missing: [] });
  assert.equal(guardLine(answer([])).state, "ok");
});

test("a repository missing required gates is red and names each kind in words", () => {
  const line = guardLine(answer(["secret_scan", "lockfile"]));
  assert.equal(line.state, "red");
  assert.equal(line.text, "secret scan, lockfile missing");
  assert.deepEqual(line.missing, ["secret scan", "lockfile"]);
});

test("a kind the harness does not know is shown as sent, with its underscores as spaces", () => {
  assert.equal(guardLine(answer(["new_kind"])).text, "new kind missing");
});

test("an answer that is not an agent-system view is refused", () => {
  assert.throws(() => guardLine(null));
  assert.throws(() => guardLine({}));
});

test("the line is blank while the answer loads", () => {
  assert.deepEqual(loadingLine(), { state: "loading", text: "", missing: [] });
});
