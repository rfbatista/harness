import { assert, file, test } from "../../../shared/testing/test.js";
import { brainstormProblem, brainstormRequest } from "./brainstorm.js";
import { noTaskProblem } from "./session.js";

file("sessions/domain/brainstorm");

test("a brainstorm needs a repository, nothing else", () => {
  assert.ok(brainstormProblem({ repositoryId: "" }).includes("repository"));
  assert.equal(brainstormProblem({ repositoryId: "r1" }), "");
});

test("the request is a product-mode start with no task, which is the one mode allowed without one", () => {
  const req = brainstormRequest({ projectId: "p1", agentId: "pm", repositoryId: "r1" });
  assert.deepEqual([req.projectId, req.ticketId, req.repositoryId, req.agentId, req.mode, req.prompt, req.autoAccept], ["p1", "", "r1", "pm", "product", "", "off"]);
  assert.ok(req.size.cols > 0 && req.size.rows > 0);
  assert.equal(noTaskProblem(req.mode), "");
  assert.equal(brainstormRequest({ projectId: "p1", repositoryId: "r1" }).agentId, "", "plain claude by default");
});
