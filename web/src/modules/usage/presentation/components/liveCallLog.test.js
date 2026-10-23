import { FeedStatus } from "../../../../shared/domain/feed.js";
import { mount } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { finished, missed, started, streamEvents } from "../../testing/liveCalls.js";
import { liveCallLog } from "./liveCallLog.js";

file("usage/presentation/components/liveCallLog");

/** A gateway whose live stream a test plays by hand. */
function fakeGateway() {
  const g = {
    opened: [],
    closed: 0,
    openLive(projectId, handlers) {
      g.opened.push({ projectId, ...handlers });
      return () => g.closed++;
    },
    get stream() {
      return g.opened[g.opened.length - 1];
    },
  };
  return g;
}

function page(over = {}) {
  const gateway = fakeGateway();
  const copied = [];
  const clipboard = { writeText: async (t) => void copied.push(t) };
  const el = document.createElement("section");
  el.dataset.projectId = "p1";
  const m = mount(liveCallLog({ gateway, clipboard, ...over }), { el });
  m.instance.init();
  return { ...m, gateway, copied, el };
}

const click = (id, extra = {}) => ({ currentTarget: { dataset: { id, ...extra } }, target: { dataset: {} } });

test("it opens stopped and holds no stream until Start", () => {
  const { instance, gateway } = page();
  assert.equal(instance.running, false);
  assert.equal(instance.toggleLabel, "Start");
  assert.equal(instance.liveText, "Stopped");
  assert.equal(gateway.opened.length, 0);
  assert.equal(instance.rows.length, 0);
});

test("Start opens the project's own stream; the indicator follows its status", () => {
  const { instance, gateway } = page();
  instance.toggle();
  assert.equal(gateway.opened.length, 1);
  assert.equal(gateway.stream.projectId, "p1");
  assert.equal(instance.toggleLabel, "Stop");
  assert.equal(instance.liveText, "Connecting…");
  gateway.stream.onStatus(FeedStatus.LIVE);
  assert.equal(instance.liveText, "Live");
  assert.equal(instance.liveTone, "success");
  gateway.stream.onStatus(FeedStatus.PAUSED);
  assert.equal(instance.liveText, "Reconnecting…");
});

test("Stop closes the stream and releases it; the calls received stay", () => {
  const { instance, gateway } = page();
  instance.toggle();
  gateway.stream.onEvent("call.started", started());
  instance.toggle();
  assert.equal(gateway.closed, 1);
  assert.equal(instance.running, false);
  assert.equal(instance.liveText, "Stopped");
  assert.equal(instance.rows.length, 1);
});

test("starting again opens a fresh stream, and a stopped stream's late events are ignored", () => {
  const { instance, gateway } = page();
  instance.toggle();
  const first = gateway.stream;
  instance.toggle();
  first.onEvent("call.started", started());
  assert.equal(instance.rows.length, 0);
  instance.toggle();
  assert.equal(gateway.opened.length, 2);
});

test("leaving the page closes the stream", () => {
  const { instance, gateway } = page();
  instance.toggle();
  instance.destroy();
  assert.equal(gateway.closed, 1);
  instance.destroy();
  assert.equal(gateway.closed, 1, "closing twice does nothing more");
});

test("a call appears in flight and is completed in place", () => {
  const { instance, gateway } = page();
  instance.toggle();
  gateway.stream.onEvent("call.started", started());
  assert.deepEqual(instance.rows.map((r) => [r.id, r.inFlight, r.status]), [["c1", true, ""]]);
  gateway.stream.onEvent("call.finished", finished());
  assert.deepEqual(instance.rows.map((r) => [r.id, r.inFlight, r.status]), [["c1", false, "200"]]);
  assert.equal(instance.count, "1 of 1 call");
});

test("a missed calls marker shows inline, in order, and counts", () => {
  const { instance, gateway } = page();
  instance.toggle();
  gateway.stream.onEvent("call.started", started({ id: "a" }));
  gateway.stream.onEvent("calls.missed", missed(3));
  gateway.stream.onEvent("call.started", started({ id: "b" }));
  assert.deepEqual(instance.rows.map((r) => r.id), ["a", "missed-1", "b"]);
  assert.equal(instance.rows[1].marker, true);
  assert.equal(instance.rows[1].text, "3 calls missed");
  assert.equal(instance.rows[0].inFlight, false, "its finish may have been missed");
});

test("only the newest 500 calls are kept", () => {
  const { instance, gateway } = page();
  instance.toggle();
  for (let i = 0; i < 503; i++) gateway.stream.onEvent("call.started", started({ id: `c${i}` }));
  assert.equal(instance.rows.length, 500);
  assert.equal(instance.rows[0].id, "c3");
});

test("a row expands to the request, one open at a time per row, and closes again", () => {
  const { instance, gateway } = page();
  instance.toggle();
  gateway.stream.onEvent("call.started", started());
  assert.equal(instance.rows[0].open, false);
  instance.expand(click("c1"));
  const row = instance.rows[0];
  assert.equal(row.open, true);
  assert.equal(row.tab, "request");
  assert.deepEqual(row.tabs.map((t) => [t.id, t.selected]), [["request", "true"], ["response", "false"], ["message", "false"], ["usage", "false"]]);
  assert.equal(row.value.method, "POST");
  instance.expand(click("c1"));
  assert.equal(instance.rows[0].open, false);
});

test("the tabs switch what the row shows: response, reassembled message, usage", () => {
  const { instance, gateway } = page();
  instance.toggle();
  gateway.stream.onEvent("call.started", started());
  gateway.stream.onEvent("call.finished", finished({ streamed: true, response: { headers: {}, body: null, events: streamEvents() } }));
  instance.expand(click("c1"));
  instance.pickTab(click("c1", { tab: "response" }));
  assert.equal(instance.rows[0].value.events.length, streamEvents().length);
  instance.pickTab(click("c1", { tab: "message" }));
  assert.equal(instance.rows[0].value.content[1].text, "Hello");
  instance.pickTab(click("c1", { tab: "usage" }));
  assert.equal(instance.rows[0].value.estimated_cost_usd, 0.01);
});

test("a tab with nothing yet says so instead of showing a tree", () => {
  const { instance, gateway } = page();
  instance.toggle();
  gateway.stream.onEvent("call.started", started());
  instance.expand(click("c1"));
  instance.pickTab(click("c1", { tab: "response" }));
  assert.equal(instance.rows[0].value, null);
  assert.equal(instance.rows[0].emptyText, "The call has not finished yet.");
  gateway.stream.onEvent("calls.missed", missed(1));
  assert.equal(instance.rows[0].emptyText, "No response: the call's finish was missed.");
});

test("an open row stays open while the call finishes and other calls arrive", () => {
  const { instance, gateway } = page();
  instance.toggle();
  gateway.stream.onEvent("call.started", started());
  instance.expand(click("c1"));
  gateway.stream.onEvent("call.started", started({ id: "c2" }));
  gateway.stream.onEvent("call.finished", finished());
  assert.equal(instance.rows[0].open, true);
  assert.equal(instance.rows[1].open, false);
});

test("expand all and collapse all fold every node under the row", () => {
  const { instance, gateway } = page();
  instance.toggle();
  gateway.stream.onEvent("call.started", started());
  instance.expand(click("c1"));
  const row = document.createElement("li");
  row.dataset.call = "";
  const tree = document.createElement("div");
  tree.className = "json-tree";
  tree.innerHTML = "<details open></details><details open></details>";
  row.append(tree);
  const button = document.createElement("button");
  row.append(button);
  instance.collapseAll({ currentTarget: button });
  assert.ok([...row.querySelectorAll("details")].every((d) => !d.open));
  instance.expandAll({ currentTarget: button });
  assert.ok([...row.querySelectorAll("details")].every((d) => d.open));
});

test("copy puts the open tab's JSON on the clipboard and says so", async () => {
  const { instance, gateway, copied } = page();
  instance.toggle();
  gateway.stream.onEvent("call.started", started());
  instance.expand(click("c1"));
  await instance.copy(click("c1"));
  assert.equal(copied.length, 1);
  assert.equal(JSON.parse(copied[0]).method, "POST");
  assert.equal(instance.rows[0].copyLabel, "Copied");
  instance.pickTab(click("c1", { tab: "usage" }));
  assert.equal(instance.rows[0].copyLabel, "Copy");
});

test("a clipboard that refuses reports it instead of failing silently", async () => {
  const { instance, gateway } = page({ clipboard: { writeText: async () => { throw new Error("denied"); } } });
  instance.toggle();
  gateway.stream.onEvent("call.started", started());
  instance.expand(click("c1"));
  await instance.copy(click("c1"));
  assert.ok(instance.error);
});

test("a tree is drawn once per value, so a redraw keeps what the person folded", () => {
  const { instance, gateway } = page();
  instance.toggle();
  gateway.stream.onEvent("call.started", started());
  instance.expand(click("c1"));
  const el = document.createElement("div");
  instance.renderTree(el, instance.rows[0]);
  const folds = el.querySelectorAll("details");
  assert.ok(folds.length > 0);
  folds[1].open = false;
  instance.renderTree(el, instance.rows[0]);
  assert.equal(el.querySelectorAll("details")[1].open, false);
  gateway.stream.onEvent("call.started", started({ id: "c2" }));
  instance.renderTree(el, instance.rows[0]);
  assert.equal(el.querySelectorAll("details")[1].open, false);
});

// search, filters, clear, auto-scroll

/** A page that has received a few calls. */
function filled() {
  const p = page();
  p.instance.toggle();
  const send = (type, data) => p.gateway.stream.onEvent(type, data);
  send("call.started", started({ id: "a", session: { id: "s1", title: "Port the parser" }, task: { id: "t1", title: "Parser" } }));
  send("call.finished", finished({ id: "a", model: "m-sonnet" }));
  send("call.started", started({ id: "b", session: { id: "s2", title: "Fix the build" }, task: { id: "t2", title: "Build" }, request: { method: "POST", path: "/v1/messages", query: "", headers: {}, body: { messages: [{ role: "user", content: "needle in here" }] } } }));
  send("call.finished", finished({ id: "b", status: 500, model: "m-opus", streamed: true }));
  send("call.started", started({ id: "c", project_id: null, session: null, task: null, kind: "system" }));
  return { ...p, send };
}

const rowIds = (instance) => instance.rows.map((r) => r.id);

test("the count reads shown of total, and a new call that does not match still counts in the total", () => {
  const { instance, send } = filled();
  assert.equal(instance.count, "3 of 3 calls");
  instance.model = "m-opus";
  assert.equal(instance.count, "1 of 3 calls");
  send("call.started", started({ id: "d" }));
  assert.equal(instance.count, "1 of 4 calls");
  assert.deepEqual(rowIds(instance), ["b"]);
});

test("the missed calls count shows apart from the count, and reads nothing when none", () => {
  const { instance, send } = filled();
  assert.equal(instance.missedText, "");
  send("calls.missed", missed(3));
  assert.equal(instance.missedText, "3 missed");
  send("calls.missed", missed(2));
  assert.equal(instance.missedText, "5 missed");
});

test("the search narrows the rows to the entries whose JSON holds it, new ones included", () => {
  const { instance, send } = filled();
  instance.query = "NEEDLE";
  assert.deepEqual(rowIds(instance), ["b"]);
  send("call.started", started({ id: "d", request: { method: "POST", path: "/v1/x", query: "", headers: {}, body: "a needle" } }));
  assert.deepEqual(rowIds(instance), ["b", "d"]);
  assert.equal(instance.count, "2 of 4 calls");
});

test("a row with a match opens to the tab holding it, and the tree gets the search", () => {
  const { instance } = filled();
  assert.equal(instance.rows[0].open, false);
  instance.query = "needle";
  const [row] = instance.rows;
  assert.equal(row.open, true);
  assert.equal(row.tab, "request");
  assert.equal(row.query, "needle");
  instance.query = "";
  assert.equal(instance.rows.find((r) => r.id === "b").open, false);
});

test("a row the person closed stays closed while the search holds", () => {
  const { instance } = filled();
  instance.query = "needle";
  instance.expand(click("b"));
  assert.equal(instance.rows[0].open, false);
  instance.expand(click("b"));
  assert.equal(instance.rows[0].open, true);
});

test("a tab the person picked is kept when the search changes", () => {
  const { instance } = filled();
  instance.query = "needle";
  instance.pickTab(click("b", { tab: "usage" }));
  instance.query = "needl";
  assert.equal(instance.rows[0].tab, "usage");
});

test("the filter options come from the entries held", () => {
  const { instance } = filled();
  assert.deepEqual(instance.options.sessions.map((o) => o.value), ["s2", "s1"]);
  assert.deepEqual(instance.options.tasks.map((o) => o.label), ["Build", "Parser"]);
  assert.deepEqual(instance.options.models, ["m-opus", "m-sonnet"]);
});

test("a chosen option stays offered after its entries are gone", () => {
  const { instance } = filled();
  instance.session = "s1";
  instance.clear();
  assert.deepEqual(instance.options.sessions.map((o) => o.value), ["s1"]);
});

test("filters by session, task, status class, streamed and no project", () => {
  const { instance } = filled();
  instance.session = "s2";
  assert.deepEqual(rowIds(instance), ["b"]);
  instance.session = "";
  instance.task = "t1";
  assert.deepEqual(rowIds(instance), ["a"]);
  instance.task = "";
  instance.statusClass = "4xx5xx";
  assert.deepEqual(rowIds(instance), ["b"]);
  instance.statusClass = "inflight";
  assert.deepEqual(rowIds(instance), ["c"]);
  instance.statusClass = "";
  instance.streamed = true;
  assert.deepEqual(rowIds(instance), ["b"]);
  instance.streamed = false;
  instance.hideNoProject = true;
  assert.deepEqual(rowIds(instance), ["a", "b"]);
});

test("a missed calls marker stays shown under a filter", () => {
  const { instance, send } = filled();
  send("calls.missed", missed(1));
  instance.model = "m-opus";
  assert.deepEqual(rowIds(instance), ["b", "missed-1"]);
});

test("Clear empties the log, the counts and the open rows; the stream keeps running", () => {
  const { instance, send, gateway } = filled();
  send("calls.missed", missed(2));
  instance.expand(click("a"));
  instance.clear();
  assert.equal(instance.rows.length, 0);
  assert.equal(instance.count, "0 of 0 calls");
  assert.equal(instance.missedText, "");
  assert.equal(instance.empty, true);
  assert.equal(instance.running, true);
  assert.equal(gateway.closed, 0);
  send("call.started", started({ id: "a" }));
  assert.equal(instance.rows[0].open, false, "a call that comes after is a new one");
});

/** A list whose newest row records that it was scrolled to. */
function scrollList(instance) {
  const scrolled = [];
  const last = { matches: (sel) => sel === "li", scrollIntoView: (o) => scrolled.push(o) };
  instance.$refs = { list: { lastElementChild: last } };
  return scrolled;
}

test("auto-scroll is on by default and follows each new entry to the newest", () => {
  const { instance, send, tick } = filled();
  assert.equal(instance.autoScroll, true);
  tick();
  const scrolled = scrollList(instance);
  send("call.started", started({ id: "d" }));
  tick();
  assert.equal(scrolled.length, 1);
});

test("a call finishing does not scroll; turned off, nothing follows", () => {
  const { instance, send, tick, set } = filled();
  tick();
  const scrolled = scrollList(instance);
  send("call.finished", finished({ id: "c" }));
  tick();
  assert.equal(scrolled.length, 0);
  set("autoScroll", false);
  send("call.started", started({ id: "d" }));
  tick();
  assert.equal(scrolled.length, 0);
});

test("turning auto-scroll back on goes to the newest at once", () => {
  const { instance, tick, set } = filled();
  tick();
  set("autoScroll", false);
  const scrolled = scrollList(instance);
  set("autoScroll", true);
  tick();
  assert.equal(scrolled.length, 1);
});
