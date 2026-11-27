import { mount } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { guardLineComponent } from "./guardLine.js";

file("agentsystem/presentation/guardLine");

const gatewayOf = (answer) => ({
  calls: [],
  async line(projectId, repositoryId) {
    this.calls.push([projectId, repositoryId]);
    if (answer instanceof Error) throw answer;
    return answer;
  },
});

const red = { state: "red", text: "secret scan missing", missing: ["secret scan"] };
const ok = { state: "ok", text: "Guard: every required gate has a command", missing: [] };

test("the line is not drawn while it loads, then reads the repository's answer", async () => {
  const gateway = gatewayOf(red);
  const { instance } = mount(() => guardLineComponent({ gateway })("p1", "r1"));
  assert.equal(instance.shown, false);
  await instance.init();
  assert.deepEqual([instance.shown, instance.red, instance.text], [true, true, "secret scan missing"]);
  assert.deepEqual(gateway.calls, [["p1", "r1"]]);
  assert.equal(instance.href, "/projects/p1/agent-system?repository=r1");
});

test("server-drawn markup names its repository in data attributes", async () => {
  const el = document.createElement("span");
  el.dataset.project = "p 1";
  el.dataset.repository = "r1";
  const gateway = gatewayOf(ok);
  const { instance } = mount(() => guardLineComponent({ gateway })(), { el });
  await instance.init();
  assert.deepEqual([instance.state, instance.red], ["ok", false]);
  assert.deepEqual(gateway.calls, [["p 1", "r1"]]);
  assert.equal(instance.href, "/projects/p%201/agent-system?repository=r1");
});

test("a failed read draws nothing", async () => {
  const { instance } = mount(() => guardLineComponent({ gateway: gatewayOf(new Error("down")) })("p1", "r1"));
  await instance.init();
  assert.equal(instance.shown, false);
});

test("without a repository nothing is asked", async () => {
  const gateway = gatewayOf(ok);
  const { instance } = mount(() => guardLineComponent({ gateway })());
  await instance.init();
  assert.deepEqual(gateway.calls, []);
});
