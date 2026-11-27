import { apiClient } from "../../../shared/infrastructure/api.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { recordingGateway } from "./recording-gateway.js";

file("tasks/infrastructure/recording-gateway");

function stub(answer) {
  const calls = [];
  const fetch = async (url, init) => {
    calls.push({ url, method: init.method, body: init.body });
    return new Response(JSON.stringify(answer), { status: 200 });
  };
  return { gateway: recordingGateway(apiClient({ base: "/api", fetch })), calls };
}

const answer = { task_id: "t1", enabled: false, explicit: null, source: "project", default: false, default_source: "project" };

test("choosing on, off or inherit is a PUT of enabled true, false or null, and the answer is read back", async () => {
  const { gateway, calls } = stub(answer);
  for (const [choice, body] of [["on", '{"enabled":true}'], ["off", '{"enabled":false}'], ["inherit", '{"enabled":null}']]) {
    const rec = await gateway.set("p1", "t1", choice);
    assert.equal(calls.at(-1).method, "PUT");
    assert.equal(calls.at(-1).url, "/api/projects/p1/tasks/t1/trajectory-recording");
    assert.equal(calls.at(-1).body, body);
    assert.equal(rec.explicit, null);
  }
});
