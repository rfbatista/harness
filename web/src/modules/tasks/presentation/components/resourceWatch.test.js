import { FeedStatus } from "../../../../shared/domain/feed.js";
import { mount } from "../../../../shared/testing/alpine.js";
import { flush } from "../../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryResources } from "../../infrastructure/memory-resources.js";
import { makeComment, makeResource } from "../../testing/resource-fixtures.js";
import { resourceWatch } from "./resourceWatch.js";

file("tasks/presentation/components/resourceWatch");

function watch() {
  const memory = memoryResources({ tickets: [{ id: "t1", projectId: "p1" }, { id: "t2", projectId: "p1" }], resources: [makeResource({ id: "r1" })], comments: [makeComment({ id: "c1" })] });
  const el = document.createElement("span");
  Object.assign(el.dataset, { projectId: "p1", ticketId: "t1", resourceIds: "r1", commentIds: "c1" });
  const { instance } = mount(resourceWatch({ resources: memory.gateway }), { el });
  instance.init();
  return { w: instance, memory };
}

test("the counts start from the ids the server rendered and follow the feed", async () => {
  const { w, memory } = watch();
  assert.deepEqual([w.resourceCount, w.commentCount], [1, 1]);
  await memory.gateway.addLink({ ticketId: "t1", url: "https://example.com" });
  await memory.gateway.addComment("t2", "another task");
  const c = await memory.gateway.addComment("t1", "hi");
  await memory.gateway.editComment(c.id, "hello"); // an edit is not one more
  await memory.gateway.removeResource("r1");
  assert.deepEqual([w.resourceCount, w.commentCount], [1, 2]);
});

test("after the feed resyncs the counts are read again", async () => {
  const { w, memory } = watch();
  memory.feedStatus(FeedStatus.PAUSED);
  w.resourceIds = [];
  memory.feedStatus(FeedStatus.RESYNCED);
  await flush();
  assert.equal(w.resourceCount, 1);
});

test("a session's comment counts like the person's; a session coming and going does not", async () => {
  const { w, memory } = watch();
  memory.startSession("t1", "s-rev");
  const c = memory.sessionAdds({ ticketId: "t1", sessionId: "s-rev", agentName: "Reviewer", body: "Found it." });
  memory.sessionEdits(c.id, "Found it twice.");
  assert.deepEqual([w.resourceCount, w.commentCount], [1, 2]);
  memory.sessionDeletes(c.id);
  memory.deleteSession("s-rev");
  assert.deepEqual([w.resourceCount, w.commentCount], [1, 1]);
});
