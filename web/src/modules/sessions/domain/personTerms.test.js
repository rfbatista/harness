import { assert, file, test } from "../../../shared/testing/test.js";
import { clampDock, DOCK_MIN_PX, endedNote, nextDockHeight, refusalCopy, SESSION_ENDED_NOTE } from "./personTerms.js";

file("sessions/domain/personTerms");

test("an ended terminal shows the server's own sentence, and offers a new one only when its shell exited or the harness restarted", () => {
  const gone = endedNote({ reason: "root_gone", message: SESSION_ENDED_NOTE });
  assert.equal(gone.strong, SESSION_ENDED_NOTE);
  assert.equal(gone.reopen, false);
  assert.equal(gone.tone, "danger");

  const exited = endedNote({ reason: "exited", message: "The shell exited." });
  assert.equal(exited.strong, "The shell exited.");
  assert.equal(exited.reopen, true);

  const stopped = endedNote({ reason: "server_stopped", message: "The harness stopped; this terminal ended." });
  assert.equal(stopped.reopen, true);

  const lost = endedNote({ reason: "restarted", message: "The harness restarted; this terminal ended." });
  assert.equal(lost.strong, "The harness restarted; this terminal ended.");
  assert.equal(lost.reopen, true);
});

test("each refusal of ADR 0051 says why the terminal is off, and the others say what to do", () => {
  for (const code of ["TERMINAL_SERVER_EXPOSED", "TERMINAL_HOST_REFUSED", "TERMINAL_ORIGIN_REFUSED"]) {
    const c = refusalCopy(code, "server words");
    assert.ok(c.title.startsWith("Terminals are off"), code);
    assert.ok(c.detail.length > 20, code);
  }
  assert.equal(refusalCopy("TERMINAL_READ_ONLY_UNAVAILABLE", "x").title, "A terminal in Resources cannot be made read-only here.");
  assert.equal(refusalCopy("ROOT_GONE", "x").title, "That folder is no longer available.");
  assert.equal(refusalCopy("PATH_NOT_FOUND", "x").title, "That folder is gone.");
});

test("a refusal this page does not know shows the server's words", () => {
  const c = refusalCopy("SOMETHING_NEW", "Because.");
  assert.equal(c.title, "Cannot open a terminal here.");
  assert.equal(c.detail, "Because.");
});

test("the dock never gets shorter than a few rows or taller than the tab", () => {
  assert.equal(clampDock(10, 800), DOCK_MIN_PX);
  assert.equal(clampDock(400, 800), 400);
  assert.equal(clampDock(5000, 800), 800);
  assert.equal(clampDock(Number.NaN, 800), DOCK_MIN_PX);
  assert.equal(clampDock(200, 50), 50, "a tab shorter than the minimum is the limit");
});

test("the arrow keys move the handle, Home and End go to the limits, and other keys do nothing", () => {
  assert.equal(nextDockHeight("ArrowUp", 300, 800), 340, "up makes the dock taller");
  assert.equal(nextDockHeight("ArrowDown", 300, 800), 260);
  assert.equal(nextDockHeight("PageUp", 300, 800), 500);
  assert.equal(nextDockHeight("Home", 300, 800), DOCK_MIN_PX);
  assert.equal(nextDockHeight("End", 300, 800), 800);
  assert.equal(nextDockHeight("ArrowDown", DOCK_MIN_PX, 800), DOCK_MIN_PX);
  assert.equal(nextDockHeight("a", 300, 800), null);
});
