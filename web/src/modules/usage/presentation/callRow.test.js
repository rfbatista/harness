import { assert, file, test } from "../../../shared/testing/test.js";
import { applyEvent, emptyLog, entriesOf } from "../domain/liveCalls.js";
import { finished, missed, started, streamEvents } from "../testing/liveCalls.js";
import { callRow, callTabs } from "./callRow.js";

file("usage/presentation/callRow");

const entryOf = (...events) => entriesOf(events.reduce((log, [type, data]) => applyEvent(log, type, data), emptyLog()))[0];
const inFlight = (over) => entryOf(["call.started", started(over)]);
const done = (startOver, finishOver) => entryOf(["call.started", started(startOver)], ["call.finished", finished(finishOver)]);

test("a call in flight shows its start, its session and task as links, and a spinner", () => {
  const row = callRow(inFlight(), "p1");
  assert.equal(row.method, "POST");
  assert.equal(row.path, "/v1/messages");
  assert.equal(row.session, "Port the parser");
  assert.equal(row.sessionHref, "/projects/p1/tasks/t1?session=s1");
  assert.equal(row.task, "Parser");
  assert.equal(row.taskHref, "/projects/p1/tasks/t1");
  assert.equal(row.inFlight, true);
  assert.equal(row.status, "");
  assert.equal(row.tokens, "");
  assert.equal(row.duration, "");
  assert.ok(/^\d\d:\d\d:\d\d$/.test(row.time), row.time);
});

test("the finished call completes in place: status, model, tokens, cost and duration", () => {
  const row = callRow(done(), "p1");
  assert.equal(row.inFlight, false);
  assert.equal(row.status, "200");
  assert.equal(row.model, "claude-sonnet");
  assert.equal(row.tokens, "15");
  assert.equal(row.cost, "$0.01");
  assert.equal(row.duration, "1.2s");
  assert.equal(row.failed, false);
});

test("an error status or a call that never reached upstream is marked failed", () => {
  assert.equal(callRow(done({}, { status: 529 }), "p1").failed, true);
  const never = callRow(done({}, { status: 0, error: "dial tcp: refused" }), "p1");
  assert.equal(never.failed, true);
  assert.equal(never.status, "dial tcp: refused");
});

test("a call whose finish was missed stops spinning and says it has no response", () => {
  const log = [["call.started", started()], ["calls.missed", missed(1)]].reduce((l, [t, d]) => applyEvent(l, t, d), emptyLog());
  const row = callRow(entriesOf(log)[0], "p1");
  assert.equal(row.inFlight, false);
  assert.equal(row.status, "no response");
});

test("a call with no session or task is the harness's own", () => {
  const row = callRow(inFlight({ session: null, task: null, kind: "system", project_id: null }), "p1");
  assert.equal(row.session, "Harness");
  assert.equal(row.sessionHref, "");
  assert.equal(row.task, "No task");
  assert.equal(row.taskHref, "");
});

test("the tabs: request, response, message and usage; only the request exists while in flight", () => {
  const tabs = callTabs(inFlight());
  assert.deepEqual(tabs.map((t) => t.id), ["request", "response", "message", "usage"]);
  assert.equal(tabs[0].value.method, "POST");
  assert.equal(tabs[1].value, null);
  assert.equal(tabs[2].value, null);
  assert.equal(tabs[3].value, null);
});

test("a JSON response is the response tab, and its message is the body", () => {
  const entry = done();
  const [, response, message, usage] = callTabs(entry);
  assert.deepEqual(response.value.headers, { "content-type": ["application/json"] });
  assert.equal(response.value.events, null);
  assert.deepEqual(message.value, entry.response.body);
  assert.equal(usage.value.input_tokens, 10);
  assert.equal(usage.value.estimated_cost_usd, 0.01);
});

test("a streamed response lists its events in order and the message is reassembled", () => {
  const events = streamEvents();
  const [, response, message] = callTabs(done({}, { streamed: true, response: { headers: {}, body: null, events } }));
  assert.deepEqual(response.value.events.map((e) => e.event), events.map((e) => e.event));
  assert.equal(message.value.content[1].text, "Hello");
  assert.equal(message.value.stop_reason, "tool_use");
});

test("a message is reassembled once per response, not per redraw", () => {
  const entry = done({}, { streamed: true, response: { headers: {}, body: null, events: streamEvents() } });
  assert.equal(callTabs(entry)[2].value, callTabs(entry)[2].value);
});
