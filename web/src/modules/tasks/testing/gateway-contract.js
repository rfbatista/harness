// The TaskGateway contract (../domain/ports.js), run by every implementation.

import { Codes } from "../../../shared/domain/errors.js";
import { assert, test } from "../../../shared/testing/test.js";
import { makeTask } from "./fixtures.js";

/**
 * @param {string} name
 * @param {(world: { projects: string[], tasks: object[], sessions: object[] }) => { gateway: import("../domain/ports.js").TaskGateway, tasks: () => object[] }} makeSubject
 */
export function taskGatewayContract(name, makeSubject) {
  const contract = (title, fn) => test(`${name} · ${title}`, fn);

  contract("listDocumentVersions lists the task's documents and their versions", async () => {
    const { gateway } = makeSubject({
      projects: ["p1"],
      tasks: [makeTask()],
      sessions: [],
      documents: [
        { id: "d1", ticketId: "t1", updatedAt: "2026-10-05T12:00:00.123456789-03:00" },
        { id: "d2", ticketId: "t2", updatedAt: "2026-10-05T12:00:00Z" },
      ],
    });
    assert.deepEqual(await gateway.listDocumentVersions("t1"), [{ id: "d1", version: "2026-10-05T12:00:00.123456789-03:00" }]);
    assert.deepEqual(await gateway.listDocumentVersions("t9"), []);
  });

  contract("createTask returns the new task", async () => {
    const { gateway } = makeSubject({ projects: ["p1"], tasks: [], sessions: [] });
    const t = await gateway.createTask({ projectId: "p1", title: "  Write docs  ", description: "the TUI section", status: "todo" });
    assert.ok(t.id);
    assert.deepEqual([t.projectId, t.title, t.description, t.status], ["p1", "Write docs", "the TUI section", "todo"]);
  });

  contract("createTask refuses a missing title, an unknown status or project", async () => {
    const { gateway } = makeSubject({ projects: ["p1"], tasks: [], sessions: [] });
    await assert.rejects(gateway.createTask({ projectId: "p1", title: " ", description: "", status: "todo" }), Codes.INVALID_INPUT);
    await assert.rejects(gateway.createTask({ projectId: "p1", title: "x", description: "", status: "someday" }), Codes.INVALID_STATUS);
    await assert.rejects(gateway.createTask({ projectId: "nope", title: "x", description: "", status: "todo" }), Codes.PROJECT_NOT_FOUND);
  });

  contract("updateTask sends only what it is given; the rest keeps its value", async () => {
    const subject = makeSubject({ projects: ["p1"], tasks: [makeTask()], sessions: [] });
    const renamed = await subject.gateway.updateTask({ id: "t1", title: "Add the SSE feed", description: "" });
    assert.deepEqual([renamed.title, renamed.description, renamed.status], ["Add the SSE feed", "", "in_progress"], "no status sent, status kept");
    const moved = await subject.gateway.moveTask("t1", "review");
    assert.deepEqual([moved.title, moved.description, moved.status], ["Add the SSE feed", "", "review"], "a move keeps the text");
    await assert.rejects(subject.gateway.updateTask({ id: "t1", title: " " }), Codes.INVALID_INPUT);
    await assert.rejects(subject.gateway.moveTask("t1", "someday"), Codes.INVALID_INPUT);
    await assert.rejects(subject.gateway.moveTask("ghost", "todo"), Codes.TICKET_NOT_FOUND);
  });

  contract("deleteTask removes it; twice is TICKET_NOT_FOUND", async () => {
    const subject = makeSubject({ projects: ["p1"], tasks: [makeTask()], sessions: [] });
    await subject.gateway.deleteTask("t1");
    assert.deepEqual(subject.tasks(), []);
    await assert.rejects(subject.gateway.deleteTask("t1"), Codes.TICKET_NOT_FOUND);
  });

  contract("countSessions counts the task's sessions and the live ones", async () => {
    const sessions = [
      { ticketId: "t1", status: "running" },
      { ticketId: "t1", status: "done" },
      { ticketId: "t2", status: "running" },
    ];
    const { gateway } = makeSubject({ projects: ["p1"], tasks: [makeTask()], sessions });
    assert.deepEqual(await gateway.countSessions("p1", "t1"), { total: 2, live: 1 });
  });

  contract("decodeTask reads the API's shape and rejects garbage", () => {
    const { gateway } = makeSubject({ projects: ["p1"], tasks: [], sessions: [] });
    assert.equal(gateway.decodeTask({ id: "t1", project_id: "p1", title: "x", status: "done" }).description, "");
    assert.throws(() => gateway.decodeTask({ id: "t1", status: "later" }), Codes.BAD_RESPONSE);
  });
}
