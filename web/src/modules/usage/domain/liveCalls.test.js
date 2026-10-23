import { assert, file, test } from "../../../shared/testing/test.js";
import { finished, missed, started } from "../testing/liveCalls.js";
import {
  MAX_ENTRIES,
  applyEvent,
  clearLog,
  countsOf,
  emptyLog,
  entriesOf,
  filterItems,
  filterOptions,
  isInFlight,
  matchTab,
  searchMatches,
} from "./liveCalls.js";

file("usage/domain/liveCalls");

const log = (...events) => events.reduce((l, [type, data]) => applyEvent(l, type, data), emptyLog());
const ids = (items) => items.map((i) => i.id);

test("a started event adds an in-flight entry", () => {
  const l = log(["call.started", started()]);
  const [e] = entriesOf(l);
  assert.equal(e.id, "c1");
  assert.ok(isInFlight(e));
  assert.equal(e.request.method, "POST");
  assert.equal(e.session.id, "s1");
  assert.equal(e.projectId, "p1");
});

test("a finished event completes the same entry in place", () => {
  const l = log(["call.started", started()], ["call.started", started({ id: "c2" })], ["call.finished", finished()]);
  assert.deepEqual(ids(entriesOf(l)), ["c1", "c2"]);
  const [c1, c2] = entriesOf(l);
  assert.ok(!isInFlight(c1));
  assert.equal(c1.status, 200);
  assert.equal(c1.model, "claude-sonnet");
  assert.equal(c1.durationMs, 1234);
  assert.equal(c1.usage.output_tokens, 5);
  assert.ok(isInFlight(c2));
});

test("a finished event with no known entry is ignored", () => {
  const l = log(["call.finished", finished({ id: "ghost" })]);
  assert.equal(entriesOf(l).length, 0);
  assert.equal(countsOf(l, {}).total, 0);
});

test("the full JSON of an entry merges both events", () => {
  const [e] = entriesOf(log(["call.started", started()], ["call.finished", finished()]));
  assert.equal(e.json.request.path, "/v1/messages");
  assert.equal(e.json.response.body.content[0].text, "Hi");
  assert.equal(e.json.status, 200);
});

test("the log keeps the newest 500 entries and drops the oldest", () => {
  let l = emptyLog();
  for (let i = 0; i < MAX_ENTRIES + 3; i++) l = applyEvent(l, "call.started", started({ id: `c${i}` }));
  const es = entriesOf(l);
  assert.equal(es.length, 500);
  assert.equal(es[0].id, "c3");
  assert.equal(es[499].id, `c${MAX_ENTRIES + 2}`);
  // a finish for a dropped entry is ignored
  l = applyEvent(l, "call.finished", finished({ id: "c0" }));
  assert.equal(entriesOf(l).length, 500);
});

test("applying an event leaves the previous log untouched", () => {
  const a = log(["call.started", started()]);
  const b = applyEvent(a, "call.finished", finished());
  assert.ok(isInFlight(entriesOf(a)[0]));
  assert.ok(!isInFlight(entriesOf(b)[0]));
});

test("unknown event types are ignored", () => {
  const l = log(["ready", { project_id: "p1" }]);
  assert.equal(l.items.length, 0);
});

test("clear empties the log and the missed count", () => {
  const l = clearLog(log(["call.started", started()], ["calls.missed", missed(4)]));
  assert.equal(l.items.length, 0);
  assert.deepEqual(countsOf(l, {}), { shown: 0, total: 0, missed: 0 });
});

// missed calls marker

test("a missed calls marker is an item in the log at its position", () => {
  const l = log(["call.started", started()], ["calls.missed", missed(3)], ["call.started", started({ id: "c2" })]);
  assert.deepEqual(l.items.map((i) => i.type), ["call", "missed", "call"]);
  assert.equal(l.items[1].count, 3);
  assert.equal(l.items[1].at.toISOString(), "2026-01-02T03:04:07.000Z");
});

test("calls in flight when the marker arrives and never finish are no longer in flight", () => {
  const l = log(["call.started", started()], ["call.started", started({ id: "c2" })], ["calls.missed", missed()], ["call.started", started({ id: "c3" })]);
  const [c1, c2, c3] = entriesOf(l);
  assert.ok(!isInFlight(c1));
  assert.ok(!isInFlight(c2));
  assert.equal(c1.noLongerInFlight, true);
  assert.equal(c1.response, null);
  assert.ok(isInFlight(c3));
});

test("a call in flight at the marker that still finishes completes normally", () => {
  const l = log(["call.started", started()], ["calls.missed", missed()], ["call.finished", finished()]);
  const [c1] = entriesOf(l);
  assert.ok(!isInFlight(c1));
  assert.equal(c1.noLongerInFlight, false);
  assert.equal(c1.status, 200);
});

test("the marker adds nothing to the entry total, and the missed total sums every marker", () => {
  const l = log(["call.started", started()], ["calls.missed", missed(3)], ["calls.missed", missed(2)]);
  assert.deepEqual(countsOf(l, {}), { shown: 1, total: 1, missed: 5 });
});

test("a marker older than every held entry is dropped with them, its count still counts", () => {
  let l = applyEvent(emptyLog(), "calls.missed", missed(7));
  for (let i = 0; i < MAX_ENTRIES; i++) l = applyEvent(l, "call.started", started({ id: `c${i}` }));
  assert.equal(l.items.filter((i) => i.type === "missed").length, 1);
  l = applyEvent(l, "call.started", started({ id: "next" }));
  assert.equal(l.items.filter((i) => i.type === "missed").length, 0);
  assert.equal(countsOf(l, {}).missed, 7);
});

// search

const paths = (entry, q) => searchMatches(entry, q).map((m) => `${m.where}:${m.path.join(".")}`);

test("search matches keys and values anywhere in the full JSON, ignoring case", () => {
  const [e] = entriesOf(log(["call.started", started()], ["call.finished", finished()]));
  assert.deepEqual(paths(e, "HELLO"), ["value:request.body.messages.0.content"]);
  assert.deepEqual(paths(e, "Messages"), ["value:request.path", "key:request.body.messages"]);
  assert.deepEqual(paths(e, "port the parser"), ["value:session.title"]);
  assert.deepEqual(paths(e, "hi"), ["value:response.body.content.0.text"]);
});

test("search reports every match, and numbers match their text", () => {
  const [e] = entriesOf(log(["call.started", started()], ["call.finished", finished()]));
  assert.deepEqual(paths(e, "1234"), ["value:duration_ms"]);
  const hits = paths(e, "claude-sonnet");
  assert.deepEqual(hits, ["value:request.body.model", "value:model"]);
});

test("a blank search matches everything with no paths; no hit gives none", () => {
  const [e] = entriesOf(log(["call.started", started()]));
  assert.deepEqual(searchMatches(e, "  "), []);
  assert.deepEqual(searchMatches(e, "zzz"), []);
});

// filters

const sample = () =>
  log(
    ["call.started", started({ id: "a", session: { id: "s1", title: "S1" }, task: { id: "t1", title: "T1" } })],
    ["call.finished", finished({ id: "a", status: 200, model: "m-sonnet", streamed: false })],
    ["call.started", started({ id: "b", session: { id: "s2", title: "S2" }, task: { id: "t2", title: "T2" } })],
    ["call.finished", finished({ id: "b", status: 429, model: "m-opus", streamed: true })],
    ["call.started", started({ id: "c", session: null, task: null, project_id: null, kind: "system" })],
    ["call.finished", finished({ id: "c", status: 0, error: "dial tcp: refused", model: "", streamed: false })],
    ["call.started", started({ id: "d" })],
    ["calls.missed", missed(1)],
    ["call.started", started({ id: "e", project_id: null })],
  );

const shownIds = (f) => ids(filterItems(sample(), f).filter((i) => i.type === "call"));

test("no filter shows every item", () => {
  assert.deepEqual(shownIds({}), ["a", "b", "c", "d", "e"]);
  assert.equal(filterItems(sample(), {}).length, 6);
});

test("filter by session, task and model", () => {
  assert.deepEqual(shownIds({ session: "s2" }), ["b"]);
  assert.deepEqual(shownIds({ task: "t2" }), ["b"]);
  assert.deepEqual(shownIds({ model: "m-opus" }), ["b"]);
});

test("filter by status class", () => {
  assert.deepEqual(shownIds({ status: "2xx" }), ["a"]);
  assert.deepEqual(shownIds({ status: "4xx5xx" }), ["b"]);
  assert.deepEqual(shownIds({ status: "error" }), ["c"]);
  assert.deepEqual(shownIds({ status: "inflight" }), ["e"]);
});

test("filter streamed, and hide calls with no project", () => {
  assert.deepEqual(shownIds({ streamed: true }), ["b"]);
  assert.deepEqual(shownIds({ hideNoProject: true }), ["a", "b", "d"]);
});

test("filters combine, and the text search joins them", () => {
  assert.deepEqual(shownIds({ hideNoProject: true, status: "4xx5xx" }), ["b"]);
  assert.deepEqual(shownIds({ session: "s1", status: "4xx5xx" }), []);
  assert.deepEqual(shownIds({ query: "s2" }), ["b"]);
  assert.deepEqual(shownIds({ query: "S2", model: "m-opus" }), ["b"]);
});

test("a marker stays in the filtered log", () => {
  const items = filterItems(sample(), { session: "s2" });
  assert.deepEqual(items.map((i) => i.type), ["call", "missed"]);
});

test("counts give shown, total and missed", () => {
  assert.deepEqual(countsOf(sample(), {}), { shown: 5, total: 5, missed: 1 });
  assert.deepEqual(countsOf(sample(), { hideNoProject: true }), { shown: 3, total: 5, missed: 1 });
});

test("filter options come from the entries held: sessions, tasks and models, once each", () => {
  const opts = filterOptions(sample());
  assert.deepEqual(opts.sessions, [{ value: "s1", label: "Port the parser" }, { value: "s2", label: "S2" }]);
  assert.deepEqual(opts.tasks, [{ value: "t1", label: "Parser" }, { value: "t2", label: "T2" }]);
  assert.deepEqual(opts.models, ["m-opus", "m-sonnet"]);
});

test("filter options of an empty log are empty, and a clear empties them", () => {
  assert.deepEqual(filterOptions(emptyLog()), { sessions: [], tasks: [], models: [] });
  assert.deepEqual(filterOptions(clearLog()), { sessions: [], tasks: [], models: [] });
});

test("a session or task without a title is labelled by its id", () => {
  const l = log(["call.started", started({ session: { id: "0123456789abcdef", title: "" }, task: { id: "t9", title: "" } })]);
  assert.deepEqual(filterOptions(l).sessions, [{ value: "0123456789abcdef", label: "Session 01234567" }]);
  assert.deepEqual(filterOptions(l).tasks, [{ value: "t9", label: "Untitled task" }]);
});

test("a new entry that does not match the filter still counts in the total", () => {
  const l = applyEvent(sample(), "call.started", started({ id: "f", session: { id: "s9", title: "S9" } }));
  assert.deepEqual(countsOf(l, { session: "s2" }), { shown: 1, total: 6, missed: 1 });
});

test("the tab a search opens: the first of request, response and usage holding a match", () => {
  const [e] = entriesOf(log(["call.started", started()], ["call.finished", finished()]));
  assert.equal(matchTab(e, "hello"), "request");
  assert.equal(matchTab(e, "content-type"), "request", "the request comes first");
  assert.equal(matchTab(e, "input_tokens"), "usage");
  assert.equal(matchTab(e, "hi"), "response", "only the response has it");
});

test("a search with no match in a tab, or a blank one, opens none", () => {
  const [e] = entriesOf(log(["call.started", started()], ["call.finished", finished()]));
  assert.equal(matchTab(e, "port the parser"), "", "the session title is in no tab");
  assert.equal(matchTab(e, "zzz"), "");
  assert.equal(matchTab(e, " "), "");
});
