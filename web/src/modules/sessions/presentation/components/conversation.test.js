import { fixedClock } from "../../../../shared/infrastructure/clock.js";
import { mount } from "../../../../shared/testing/alpine.js";
import { flush } from "../../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryArtifacts } from "../../infrastructure/memory-artifacts.js";
import { makeArtifact } from "../../testing/artifact-fixtures.js";
import { makeMessage } from "../../testing/channel-fixtures.js";
import { makeSession, T0 } from "../../testing/fixtures.js";
import { conversation } from "./conversation.js";

file("sessions/presentation/conversation");

const sessions = [
  makeSession({ id: "arch", role: "architect", mode: "architect", agentId: "lead" }),
  makeSession({ id: "d1", role: "delegate", parentSessionId: "arch", agentId: "go" }),
  makeSession({ id: "d2", role: "delegate", parentSessionId: "arch", agentId: "web" }),
];
const at = (m) => new Date(T0.getTime() + m * 60_000);
const messages = [
  makeMessage({ id: "1", fromSessionId: "d1", toSessionId: "arch", createdAt: at(1), artifactIds: ["art1"] }),
  makeMessage({ id: "2", fromSessionId: "d2", toSessionId: "arch", createdAt: at(2) }),
  makeMessage({ id: "3", fromSessionId: "arch", toSessionId: "d1", kind: "reply", status: "", createdAt: at(3) }),
];

function make(panel, opts) {
  const channelStore = { messages: opts?.list ?? messages, loaded: opts?.loaded ?? true };
  const art = memoryArtifacts({ artifacts: opts?.artifacts ?? [] });
  const mounted = mount(() => conversation({ artifacts: art.gateway, channelStore, clock: fixedClock(at(5)) })(panel));
  mounted.instance.init();
  return { ...mounted, channelStore };
}

const base = {
  sessions,
  agentNames: { lead: "software-architect", go: "go-developer", web: "frontend-developer" },
  documentTitles: {},
  projectId: "p1",
  ticketId: "t1",
};

test("on the architect: every message, oldest first, narrowed to one delegate by the filter", () => {
  const { instance } = make({ ...base, sessionId: "arch", isArchitect: true, delegates: [{ id: "d1", label: "go-developer" }, { id: "d2", label: "frontend-developer" }] });
  assert.equal(instance.showsFilter, true);
  assert.deepEqual(instance.views.map((v) => v.id), ["1", "2", "3"]);
  instance.filter = "d1";
  assert.deepEqual(instance.views.map((v) => `${v.from}→${v.to}`), ["go-developer→the architect", "the architect→go-developer"]);
  instance.destroy();
});

test("on a delegate: its own thread with the architect, without a filter", () => {
  const { instance } = make({ ...base, sessionId: "d2", isArchitect: false, delegates: [] });
  assert.equal(instance.showsFilter, false);
  assert.deepEqual(instance.views.map((v) => v.id), ["2"]);
  instance.destroy();
});

test("empty once loaded, not before", () => {
  const before = make({ ...base, sessionId: "d2" }, { list: [], loaded: false }).instance;
  assert.deepEqual([before.loading, before.isEmpty], [true, false]);
  const after = make({ ...base, sessionId: "d2" }, { list: [], loaded: true }).instance;
  assert.deepEqual([after.loading, after.isEmpty], [false, true]);
});

test("names the artifacts a message points at by listing its sender's, once", async () => {
  const { instance } = make(
    { ...base, sessionId: "arch", isArchitect: true },
    { artifacts: [makeArtifact({ id: "art1", sessionId: "d1", title: "Flow diagram" })] },
  );
  await flush();
  const [first] = instance.views;
  assert.deepEqual(first.artifacts, [{ id: "art1", title: "Flow diagram", href: "/api/artifacts/art1/view/" }]);
  instance.destroy();
});

test("a new message is shown as it lands in the store", () => {
  const { instance, channelStore } = make({ ...base, sessionId: "d1" });
  assert.equal(instance.count, 2);
  channelStore.messages = [...messages, makeMessage({ id: "4", fromSessionId: "d1", createdAt: at(4), body: "Done." })];
  assert.equal(instance.count, 3);
  assert.equal(instance.views.at(-1).body, "Done.");
  instance.destroy();
});

test("a long message is folded until the reader shows it all", () => {
  const long = makeMessage({ id: "long", fromSessionId: "d1", body: Array(12).fill("step").join("\n") });
  const { instance } = make({ ...base, sessionId: "d1" }, { list: [long] });
  const [v] = instance.views;
  assert.deepEqual([instance.clamped(v), instance.toggleWord(v)], [true, "Show all"]);
  instance.toggle("long");
  assert.deepEqual([instance.clamped(v), instance.expanded(v), instance.toggleWord(v)], [false, true, "Show less"]);
  instance.destroy();
});
