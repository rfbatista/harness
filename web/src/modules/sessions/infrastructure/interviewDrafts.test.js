import { assert, file, test } from "../../../shared/testing/test.js";
import { blankAnswer } from "../domain/answers.js";
import { DRAFT_DAYS, DRAFT_PREFIX, interviewDrafts } from "./interviewDrafts.js";

file("sessions/infrastructure/interviewDrafts");

const DAY = 86_400_000;

function memoryStorage(entries = {}) {
  const data = new Map(Object.entries(entries));
  return {
    data,
    getItem: (k) => data.get(k) ?? null,
    setItem: (k, v) => data.set(k, String(v)),
    removeItem: (k) => data.delete(k),
    get length() {
      return data.size;
    },
    key: (n) => [...data.keys()][n] ?? null,
  };
}

const answers = [{ ...blankAnswer(), chosen: [1] }, { ...blankAnswer(), text: "typed", note: "n" }];

test("unsent answers are kept per interview id and read back", () => {
  const storage = memoryStorage();
  const drafts = interviewDrafts({ storage, now: () => 1_000 });
  drafts.write("iv-1", answers);
  assert.ok(storage.data.has(`${DRAFT_PREFIX}iv-1`));
  assert.deepEqual(drafts.read("iv-1", 2), answers);
  assert.equal(drafts.read("iv-2", 2), null);
});

test("a draft older than 7 days is dropped when read, and a newer one is kept", () => {
  const storage = memoryStorage();
  interviewDrafts({ storage, now: () => 0 }).write("iv-1", answers);
  assert.deepEqual(interviewDrafts({ storage, now: () => DRAFT_DAYS * DAY - 1 }).read("iv-1", 2), answers);
  assert.equal(interviewDrafts({ storage, now: () => DRAFT_DAYS * DAY + 1 }).read("iv-1", 2), null);
  assert.equal(storage.data.size, 0, "the stale entry is removed, not just ignored");
});

test("writing again starts the 7 days again", () => {
  const storage = memoryStorage();
  interviewDrafts({ storage, now: () => 0 }).write("iv-1", answers);
  interviewDrafts({ storage, now: () => 6 * DAY }).write("iv-1", answers);
  assert.ok(interviewDrafts({ storage, now: () => 10 * DAY }).read("iv-1", 2) !== null);
});

test("a draft that does not fit the interview (other question count, junk) is dropped, never trusted", () => {
  const storage = memoryStorage({ [`${DRAFT_PREFIX}a`]: "not json", [`${DRAFT_PREFIX}b`]: JSON.stringify({ at: 1, answers: [{ chosen: "x" }] }), [`${DRAFT_PREFIX}c`]: JSON.stringify({ at: 1, answers }) });
  const drafts = interviewDrafts({ storage, now: () => 2 });
  assert.equal(drafts.read("a", 2), null);
  assert.equal(drafts.read("b", 1), null);
  assert.equal(drafts.read("c", 3), null, "three questions now, two kept");
  assert.equal(storage.data.size, 0);
});

test("drop removes one interview's draft and nothing else", () => {
  const storage = memoryStorage({ other: "x" });
  const drafts = interviewDrafts({ storage, now: () => 1 });
  drafts.write("iv-1", answers);
  drafts.write("iv-2", answers);
  drafts.drop("iv-1");
  assert.equal(drafts.read("iv-1", 2), null);
  assert.ok(drafts.read("iv-2", 2) !== null);
  assert.equal(storage.data.get("other"), "x");
});

test("the sweep drops every expired draft and leaves the live ones and other keys", () => {
  const storage = memoryStorage({ other: "x" });
  interviewDrafts({ storage, now: () => 0 }).write("old", answers);
  interviewDrafts({ storage, now: () => 5 * DAY }).write("new", answers);
  interviewDrafts({ storage, now: () => 8 * DAY }).sweep();
  assert.deepEqual([...storage.data.keys()].sort(), [`${DRAFT_PREFIX}new`, "other"]);
});

test("storage that is missing or throws never breaks answering", () => {
  const broken = { getItem() { throw new Error("blocked"); }, setItem() { throw new Error("blocked"); }, removeItem() { throw new Error("blocked"); }, get length() { throw new Error("blocked"); }, key() { throw new Error("blocked"); } };
  for (const storage of [null, broken]) {
    const drafts = interviewDrafts({ storage });
    drafts.write("iv-1", answers);
    drafts.drop("iv-1");
    drafts.sweep();
    assert.equal(drafts.read("iv-1", 2), null);
  }
});
