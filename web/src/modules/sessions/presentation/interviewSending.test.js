import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { flush } from "../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { makeInterview, makeList } from "../testing/interview-fixtures.js";
import { interviewStore } from "./interviewStore.js";

file("sessions/presentation/interviewSending");

/** A gateway the test drives: submit and dismiss wait for `reply`, record what they were asked, and answer as told. */
function fakeGateway() {
  const gw = {
    posts: [],
    pending: [],
    followers: [],
    async list() {
      return { seq: 0, interviews: [] };
    },
    follow(id, onEvent, onStatus) {
      gw.followers.push({ id, onEvent, onStatus });
      return () => {};
    },
    submit: (id, answers) => gw.post("submit", id, { answers }),
    dismiss: (id, note) => gw.post("dismiss", id, { note }),
    post(verb, id, body) {
      gw.posts.push({ verb, id, ...body });
      return new Promise((resolve, reject) => gw.pending.push({ resolve, reject }));
    },
    /** Answers the oldest post still waiting. */
    ok: (interview) => gw.pending.shift().resolve(interview),
    fail: (err) => gw.pending.shift().reject(err),
  };
  return gw;
}

const closedAs = (id, state, over = {}) =>
  makeInterview({ id, state, ended_at: "2026-11-08T14:41:00Z", ...(state === "sent" ? { delivery: "queued", answers: [{ chosen: [1], text: "", note: "", skipped: false }, { chosen: [], text: "", note: "", skipped: true }] } : {}), ...over });

function setup({ interviews = [makeInterview({ id: "a" })], visible = true, live = true } = {}) {
  const gateway = fakeGateway();
  const store = interviewStore({ gateway, prefs: { interviewHidden: () => false, setInterviewHidden() {} }, timers: { setTimeout: () => 0, clearTimeout() {} }, visible: () => visible });
  store.seed({ s1: makeList(1, ...interviews) });
  if (live) store.connection = "live";
  const said = [];
  return { store, gateway, said, announce: (t) => said.push(t) };
}

/** Both questions of interview a answered. */
const answerAll = (store, id = "a") => {
  store.choose(id, 0, "1");
  store.type(id, 1, "in the page seed");
};

const rows = (store) => store.view("s1").history.rows.map((r) => [r.id, r.state]);
const phase = (store, id = "a") => store.step(id).phase;

test("a fully answered interview is posted as the contract's answers and leaves the open ones at once", async () => {
  const { store, gateway, announce, said } = setup();
  answerAll(store);
  const sent = store.send("s1", "a", announce);
  assert.equal(phase(store), "sending");
  assert.deepEqual(gateway.posts, [{ verb: "submit", id: "a", answers: [{ chosen: [1], text: "", note: "", skipped: false }, { chosen: [], text: "in the page seed", note: "", skipped: false }] }]);
  gateway.ok(closedAs("a", "sent"));
  await sent;
  assert.equal(phase(store), "idle");
  assert.deepEqual(rows(store), [["a", "sent"]]);
  assert.equal(store.view("s1").cards.length, 0);
  assert.equal(store.view("s1").history.rows[0].opened, true, "the sent card is open to read");
  assert.equal(store.answers.a, undefined, "its draft is let go");
  assert.deepEqual(said, ["Answers sent"]);
});

test("a sent interview shows its banner here, with its delivery as the server said it", async () => {
  const { store, gateway } = setup();
  answerAll(store);
  const sent = store.send("s1", "a");
  gateway.ok(closedAs("a", "sent"));
  await sent;
  const row = store.view("s1").history.rows[0];
  assert.equal(row.banner, "Answers sent. The agent reads them as its next message, after its current turn.");
  assert.equal(row.sub, "1 answered, 1 skipped · queued: the agent is mid-turn");
});

test("blanks ask first: nothing is posted until the confirm, and Review skipped goes to the first blank", async () => {
  const { store, gateway } = setup();
  store.choose("a", 0, "1");
  await store.send("s1", "a");
  assert.equal(phase(store), "confirming");
  assert.deepEqual(gateway.posts, []);
  assert.equal(store.footer("a").confirm.title, "Send with 1 skipped?");
  assert.equal(store.footer("a").confirm.send, "Send 2 answers");
  assert.equal(store.review("a"), 1, "the second question is the first blank");
  assert.equal(phase(store), "idle");
  void store.send("s1", "a");
  void store.send("s1", "a");
  assert.equal(phase(store), "sending", "the second send is the confirm");
  assert.equal(gateway.posts.length, 1);
});

test("sending an interview already sending posts once", async () => {
  const { store, gateway } = setup();
  answerAll(store);
  void store.send("s1", "a");
  void store.send("s1", "a");
  void store.send("s1", "a");
  assert.equal(gateway.posts.length, 1);
});

test("while it sends the fields are locked and Dismiss is off", () => {
  const { store } = setup();
  answerAll(store);
  void store.send("s1", "a");
  const f = store.footer("a");
  assert.deepEqual([f.phase, f.locked, f.busy, f.dismissDisabled, f.status], ["sending", true, true, true, "Sending your answers…"]);
  store.askDismiss("a");
  assert.equal(store.step("a").kind, "send", "a dismiss cannot begin mid-send");
});

test("a failed send keeps the answers and shows the code; Try again posts the same answers and succeeds once", async () => {
  const { store, gateway } = setup();
  answerAll(store);
  const first = store.send("s1", "a");
  gateway.fail(new StructuredError(Codes.NETWORK, "The harness server is not reachable."));
  await first;
  const f = store.footer("a");
  assert.deepEqual([f.phase, f.error.code, f.locked], ["failed", "INTERVIEW_SEND_FAILED", false]);
  assert.equal(f.error.title, "Could not send your answers.");
  assert.equal(f.error.text, "The harness did not answer. Your answers are kept here.");
  assert.equal(f.error.tail, "INTERVIEW_SEND_FAILED · try again");
  assert.equal(f.send, "Try again");
  assert.deepEqual(store.answers.a.map((s) => [s.chosen, s.text]), [[[1], ""], [[], "in the page seed"]]);
  const again = store.retry("s1", "a");
  assert.equal(phase(store), "sending");
  gateway.ok(closedAs("a", "sent"));
  await again;
  assert.deepEqual(gateway.posts.map((p) => p.verb + p.id), ["submita", "submita"]);
  assert.deepEqual(gateway.posts[0].answers, gateway.posts[1].answers);
  assert.deepEqual(rows(store), [["a", "sent"]]);
});

test("the server's echo that beats its answer moves the card without announcing another tab, and the answer then agrees", async () => {
  const { store, gateway, announce, said } = setup();
  answerAll(store);
  const sent = store.send("s1", "a", announce);
  store.receive("s1", { seq: 2, kind: "interview_changed", interview: closedAs("a", "sent") }, announce);
  assert.deepEqual(rows(store), [["a", "sent"]]);
  assert.deepEqual(said, [], "this tab's own send is not another tab's");
  gateway.ok(closedAs("a", "sent"));
  await sent;
  assert.deepEqual(said, ["Answers sent"]);
  assert.deepEqual(rows(store), [["a", "sent"]]);
  assert.equal(store.list("s1").seq, 2);
});

test("an answer older than the echo does not take the delivery back", async () => {
  const { store, gateway, announce } = setup();
  answerAll(store);
  const sent = store.send("s1", "a", announce);
  store.receive("s1", { seq: 2, kind: "interview_changed", interview: closedAs("a", "sent", { delivery: "delivered", delivered_at: "2026-11-08T14:42:00Z" }) }, announce);
  gateway.ok(closedAs("a", "sent", { delivery: "queued" }));
  await sent;
  assert.equal(store.view("s1").history.rows[0].sub.includes("delivered"), true);
});

test("a change from another tab to an interview not in flight here is still announced", () => {
  const { store, announce, said } = setup({ interviews: [makeInterview({ id: "a" }), makeInterview({ id: "b" })] });
  answerAll(store, "a");
  void store.send("s1", "a", announce);
  store.receive("s1", { seq: 2, kind: "interview_changed", interview: closedAs("b", "sent") }, announce);
  assert.deepEqual(said, ["Interview answered in another tab"]);
});

test("a send refused because the session ended makes the interview Expired, with what was typed to copy", async () => {
  const { store, gateway, announce, said } = setup();
  answerAll(store);
  const sent = store.send("s1", "a", announce);
  gateway.fail(new StructuredError("INTERVIEW_EXPIRED", "the session ended before the answer", 409, { interview: closedAs("a", "expired") }));
  await sent;
  assert.deepEqual(rows(store), [["a", "expired"]]);
  const row = store.view("s1").history.rows[0];
  assert.deepEqual([row.copy, row.opened, row.questions.map((q) => q.answer)], [true, true, ["The harness database", "in the page seed"]]);
  assert.deepEqual(said, ["Interview expired: the session ended"]);
  assert.equal(phase(store), "idle");
  assert.deepEqual([row.banner, row.bannerTone, row.bannerRole], ["The session ended before the agent could read your answers. Copy them if you need them.", "danger", "alert"]);
});

test("a send refused as not open shows the state the server says it is in", async () => {
  const { store, gateway } = setup();
  answerAll(store);
  const sent = store.send("s1", "a");
  gateway.fail(new StructuredError("INTERVIEW_NOT_OPEN", "the interview is dismissed, not open", 409, { interview: closedAs("a", "dismissed") }));
  await sent;
  assert.deepEqual(rows(store), [["a", "dismissed"]]);
});

test("answers the server finds invalid are shown as it said, kept, with no retry to loop on", async () => {
  const { store, gateway } = setup();
  answerAll(store);
  const sent = store.send("s1", "a");
  gateway.fail(new StructuredError("INTERVIEW_ANSWERS_INVALID", "answer 2 is too long", 400));
  await sent;
  const f = store.footer("a");
  assert.deepEqual([f.phase, f.error.title, f.error.text, f.error.tail, f.send, f.locked], ["failed", "Could not send your answers.", "answer 2 is too long", "INTERVIEW_ANSWERS_INVALID", "Send answers", false]);
  assert.equal(store.view("s1").cards.length, 1, "still open, still editable");
  await store.retry("s1", "a");
  assert.equal(gateway.posts.length, 1, "there is nothing to retry");
  store.type("a", 1, "shorter");
  const again = store.send("s1", "a");
  assert.equal(gateway.posts.length, 2);
  gateway.ok(closedAs("a", "sent"));
  await again;
});

test("while the stream is not live, Send is disabled with its reason and sends nothing; reading and typing go on", async () => {
  const { store, gateway } = setup();
  answerAll(store);
  for (const status of ["paused", "connecting"]) {
    store.connection = status;
    const f = store.footer("a");
    assert.deepEqual([f.sendDisabled, f.reason], [true, "Waiting for the connection"], status);
    await store.send("s1", "a");
    assert.equal(gateway.posts.length, 0);
    store.type("a", 1, "still typing " + status);
  }
  store.connection = "live";
  assert.deepEqual([store.footer("a").sendDisabled, store.footer("a").reason], [false, ""]);
});

test("the connection follows the interview feed's status", () => {
  const { store, gateway } = setup({ live: false });
  store.follow("s1", () => {});
  assert.equal(store.connection, "connecting");
  gateway.followers[0].onStatus("live");
  assert.equal(store.connection, "live");
  gateway.followers[0].onStatus("paused");
  assert.equal(store.footer("a").sendDisabled, true);
});

test("dismiss asks first with an optional note; keeping the answers goes back and loses nothing", () => {
  const { store, gateway } = setup();
  answerAll(store);
  store.askDismiss("a");
  const f = store.footer("a");
  assert.deepEqual([f.phase, f.dismiss.title, f.dismiss.text, f.dismiss.keep, f.dismiss.confirm], [
    "confirming",
    "Dismiss this interview?",
    "The agent is told you chose not to answer. Your typed answers are discarded. You cannot undo it.",
    "Keep answering",
    "Dismiss interview",
  ]);
  store.keepAnswering("a");
  assert.equal(phase(store), "idle");
  assert.equal(store.touched("a"), true);
  assert.equal(gateway.posts.length, 0);
});

test("dismissing with a note posts it; without one posts an empty note; the draft is dropped and the row reads dismissed", async () => {
  const { store, gateway, announce, said } = setup({ interviews: [makeInterview({ id: "a" }), makeInterview({ id: "b" })] });
  answerAll(store, "a");
  store.askDismiss("a");
  store.typeDismissNote("a", "not now");
  const one = store.confirmDismiss("s1", "a", announce);
  assert.deepEqual(gateway.posts[0], { verb: "dismiss", id: "a", note: "not now" });
  gateway.ok(closedAs("a", "dismissed", { delivery: "delivered", delivered_at: "2026-11-08T14:41:00Z", dismiss_note: "not now" }));
  await one;
  store.askDismiss("b");
  const two = store.confirmDismiss("s1", "b", announce);
  assert.deepEqual(gateway.posts[1], { verb: "dismiss", id: "b", note: "" });
  gateway.ok(closedAs("b", "dismissed", { delivery: "delivered", delivered_at: "2026-11-08T14:41:00Z" }));
  await two;
  assert.deepEqual(rows(store).map((r) => r[1]), ["dismissed", "dismissed"]);
  assert.equal(store.answers.a, undefined);
  assert.equal(store.view("s1").history.rows.find((r) => r.id === "a").sub.startsWith("Dismissed "), true);
  assert.deepEqual(said, ["Interview dismissed", "Interview dismissed"]);
});

test("a failed dismiss says so the same way, keeps the note, and Try again dismisses once", async () => {
  const { store, gateway } = setup();
  store.askDismiss("a");
  store.typeDismissNote("a", "later");
  const first = store.confirmDismiss("s1", "a");
  gateway.fail(new StructuredError("INTERNAL", "boom", 500));
  await first;
  const f = store.footer("a");
  assert.deepEqual([f.phase, f.error.title, f.error.tail, f.send], ["failed", "Could not dismiss the interview.", "INTERVIEW_SEND_FAILED · try again", "Try again"]);
  assert.equal(store.dismissNoteOf("a"), "later");
  const again = store.retry("s1", "a");
  assert.deepEqual(gateway.posts.map((p) => p.verb + ":" + p.note), ["dismiss:later", "dismiss:later"]);
  gateway.ok(closedAs("a", "dismissed", { delivery: "delivered", delivered_at: "2026-11-08T14:41:00Z" }));
  await again;
  assert.deepEqual(rows(store), [["a", "dismissed"]]);
  await flush();
});
