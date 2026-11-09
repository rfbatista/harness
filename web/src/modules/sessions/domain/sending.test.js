import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { makeInterview } from "../testing/interview-fixtures.js";
import { canSend, failureOf, idle, reduce } from "./sending.js";

file("sessions/domain/sending");

const walk = (...actions) => actions.reduce(reduce, idle);
const at = (s) => [s.phase, s.kind];

test("a fully answered interview goes straight to sending; one with blanks asks first", () => {
  assert.deepEqual(at(walk({ type: "send", skipped: 0 })), ["sending", "send"]);
  assert.deepEqual(at(walk({ type: "send", skipped: 2 })), ["confirming", "send"]);
});

test("the confirm sends, or goes back to review the blanks", () => {
  assert.deepEqual(at(walk({ type: "send", skipped: 2 }, { type: "confirm" })), ["sending", "send"]);
  assert.deepEqual(at(walk({ type: "send", skipped: 2 }, { type: "review" })), ["idle", null]);
});

test("a second send while it confirms is the confirm, and while it sends changes nothing", () => {
  assert.deepEqual(at(walk({ type: "send", skipped: 2 }, { type: "send", skipped: 2 })), ["sending", "send"]);
  const sending = walk({ type: "send", skipped: 0 });
  assert.equal(reduce(sending, { type: "send", skipped: 0 }), sending);
  assert.equal(reduce(sending, { type: "dismiss" }), sending, "Dismiss is disabled while it sends");
});

test("a failed send keeps what the person confirmed: Try again sends without asking again", () => {
  const failed = walk({ type: "send", skipped: 2 }, { type: "confirm" }, { type: "failed", error: { code: "INTERVIEW_SEND_FAILED", message: "x", retry: true } });
  assert.deepEqual(at(failed), ["failed", "send"]);
  assert.equal(failed.error.code, "INTERVIEW_SEND_FAILED");
  assert.deepEqual(at(reduce(failed, { type: "retry" })), ["sending", "send"]);
});

test("a send refused as invalid is not retried: sending again is a new send, asking about the blanks", () => {
  const invalid = walk({ type: "send", skipped: 0 }, { type: "failed", error: { code: "INTERVIEW_ANSWERS_INVALID", message: "too long", retry: false } });
  assert.deepEqual(at(reduce(invalid, { type: "retry" })), ["failed", "send"], "there is nothing to retry");
  assert.deepEqual(at(reduce(invalid, { type: "send", skipped: 1 })), ["confirming", "send"]);
});

test("dismiss asks first; keeping the answers goes back; confirming dismisses; a failure can be retried", () => {
  assert.deepEqual(at(walk({ type: "dismiss" })), ["confirming", "dismiss"]);
  assert.deepEqual(at(walk({ type: "dismiss" }, { type: "keep" })), ["idle", null]);
  const sending = walk({ type: "dismiss" }, { type: "confirm" });
  assert.deepEqual(at(sending), ["sending", "dismiss"]);
  const failed = reduce(sending, { type: "failed", error: { code: "INTERVIEW_SEND_FAILED", message: "x", retry: true } });
  assert.deepEqual(at(failed), ["failed", "dismiss"]);
  assert.deepEqual(at(reduce(failed, { type: "retry" })), ["sending", "dismiss"]);
});

test("a finished send or dismiss leaves nothing behind", () => {
  assert.deepEqual(at(walk({ type: "send", skipped: 0 }, { type: "done" })), ["idle", null]);
});

test("Send waits for the connection: only a live stream may send", () => {
  assert.deepEqual([canSend("live"), canSend("resynced"), canSend("paused"), canSend("connecting")], [true, true, false, false]);
});

test("a network or server failure is the UI's INTERVIEW_SEND_FAILED, to try again", () => {
  assert.deepEqual(failureOf(new StructuredError(Codes.NETWORK, "The harness server is not reachable.")), { code: "INTERVIEW_SEND_FAILED", message: "", retry: true, closed: null });
  assert.equal(failureOf(new StructuredError("INTERNAL", "boom", 500)).code, "INTERVIEW_SEND_FAILED");
  assert.equal(failureOf(new Error("who knows")).retry, true);
});

test("a coded refusal carries what the server said; an expired or closed interview carries the interview", () => {
  const sent = makeInterview({ state: "sent", ended_at: "2026-11-08T14:41:00Z", delivery: "queued" });
  const invalid = failureOf(new StructuredError("INTERVIEW_ANSWERS_INVALID", "answer 2 is too long", 400));
  assert.deepEqual(invalid, { code: "INTERVIEW_ANSWERS_INVALID", message: "answer 2 is too long", retry: false, closed: null });
  const expired = failureOf(new StructuredError("INTERVIEW_EXPIRED", "the session ended", 409, { interview: sent }));
  assert.equal(expired.closed.state, "sent");
  assert.equal(expired.retry, false);
  const notOpen = failureOf(new StructuredError("INTERVIEW_NOT_OPEN", "it is dismissed", 409, { interview: sent }));
  assert.equal(notOpen.closed.id, "iv-1");
});
