import { Codes } from "../../../shared/domain/errors.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { makeArtifact, toArtifactDTO } from "../testing/artifact-fixtures.js";
import { toArtifact, toArtifactAnswer, toArtifactEvent, toArtifactList, toProjectArtifactChange } from "./artifact-dto.js";

file("sessions/infrastructure/artifact-dto");

test("reads the contract's resource and points src at the view route", () => {
  const a = toArtifact(toArtifactDTO(makeArtifact({ id: "a 1", kind: "image", path: "shots/hero.png", mime: "image/png", sizeBytes: 12 })));
  assert.equal(a.src, "/api/artifacts/a%201/view/");
  assert.equal(a.kind, "image");
  assert.equal(a.path, "shots/hero.png");
  assert.equal(a.url, "");
  assert.equal(a.sizeBytes, 12);
  assert.ok(a.updatedAt instanceof Date && a.createdAt instanceof Date);
  assert.ok(Object.isFrozen(a));
});

test("a url artifact's src is the url itself; path is empty", () => {
  const a = toArtifact(toArtifactDTO(makeArtifact({ kind: "url", path: "", url: "http://localhost:5173/", mime: "", sizeBytes: 0 })));
  assert.equal(a.src, "http://localhost:5173/");
  assert.equal(a.path, "");
});

test("missing optional fields read as empty", () => {
  const a = toArtifact({ id: "x", kind: "file", revision: 1, updated_at: "2026-10-05T12:00:00Z", created_at: "2026-10-05T12:00:00Z" });
  assert.deepEqual([a.title, a.note, a.path, a.url, a.mime, a.sizeBytes, a.sessionId], ["", "", "", "", "", 0, ""]);
});

test("garbage is BAD_RESPONSE", () => {
  const ok = toArtifactDTO(makeArtifact());
  assert.throws(() => toArtifact({ ...ok, id: "" }), Codes.BAD_RESPONSE);
  assert.throws(() => toArtifact({ ...ok, kind: "gif" }), Codes.BAD_RESPONSE);
  assert.throws(() => toArtifact({ ...ok, revision: 0 }), Codes.BAD_RESPONSE);
  assert.throws(() => toArtifact({ ...ok, updated_at: "yesterday" }), Codes.BAD_RESPONSE);
  assert.throws(() => toArtifactList({ items: [] }), Codes.BAD_RESPONSE);
});

test("events: artifact → published, done → ended, anything else → null", () => {
  const dto = toArtifactDTO(makeArtifact({ revision: 3 }));
  const published = toArtifactEvent({ seq: 42, session_id: "s1", type: "artifact", text: "First cut", artifact: dto, at: "2026-10-05T12:00:00Z" });
  assert.equal(published.kind, "published");
  assert.equal(published.artifact.revision, 3);
  assert.deepEqual(toArtifactEvent({ seq: 43, type: "done", session_id: "s1", at: "2026-10-05T12:00:00Z" }), { kind: "ended" });
  assert.equal(toArtifactEvent({ seq: 1, type: "status", status: "running" }), null);
  assert.equal(toArtifactEvent(null), null);
  assert.throws(() => toArtifactEvent({ type: "artifact", artifact: { id: "" } }), Codes.BAD_RESPONSE);
});

test("scope is read; a server that does not send it reads as task; anything else is BAD_RESPONSE", () => {
  const dto = toArtifactDTO(makeArtifact({ scope: "project" }));
  assert.equal(toArtifact(dto).scope, "project");
  const { scope: _, ...older } = dto;
  assert.equal(toArtifact(older).scope, "task");
  assert.throws(() => toArtifact({ ...dto, scope: "global" }), Codes.BAD_RESPONSE);
});

test("set_artifact_scope answers {artifact}", () => {
  const dto = toArtifactDTO(makeArtifact({ id: "a1", scope: "project" }));
  const a = toArtifactAnswer({ artifact: dto });
  assert.deepEqual([a.id, a.scope], ["a1", "project"]);
  assert.throws(() => toArtifactAnswer({ document: dto }), Codes.BAD_RESPONSE);
});

test("attached_ticket_ids reads as a frozen list; a server from before attachments reads as none", () => {
  const dto = toArtifactDTO(makeArtifact({ scope: "project", attachedTicketIds: ["t2", "t3"] }));
  const a = toArtifact(dto);
  assert.deepEqual(a.attachedTicketIds, ["t2", "t3"]);
  assert.ok(Object.isFrozen(a.attachedTicketIds));
  const { attached_ticket_ids: _, ...old } = dto;
  assert.deepEqual(toArtifact(old).attachedTicketIds, []);
  assert.deepEqual(toArtifact({ ...dto, attached_ticket_ids: null }).attachedTicketIds, []);
});

test("malformed attached_ticket_ids is BAD_RESPONSE", () => {
  const dto = toArtifactDTO(makeArtifact());
  assert.throws(() => toArtifact({ ...dto, attached_ticket_ids: "t2" }), Codes.BAD_RESPONSE);
  assert.throws(() => toArtifact({ ...dto, attached_ticket_ids: ["t2", 3] }), Codes.BAD_RESPONSE);
  assert.throws(() => toArtifact({ ...dto, attached_ticket_ids: [""] }), Codes.BAD_RESPONSE);
});

test("project feed: an artifact change is changed, its deleted form keeps the ids, other keys are null", () => {
  const dto = toArtifactDTO(makeArtifact({ id: "a", scope: "project", attachedTicketIds: ["t2"] }));
  const changed = toProjectArtifactChange({ artifact: dto });
  assert.equal(changed.kind, "changed");
  assert.deepEqual(changed.artifact.attachedTicketIds, ["t2"]);
  assert.equal(changed.artifact.src, "/api/artifacts/a/view/");

  const deleted = toProjectArtifactChange({ artifact: { id: "a", project_id: "p1", ticket_id: "t1", attached_ticket_ids: ["t2"] }, deleted: true });
  assert.deepEqual(deleted, { kind: "deleted", id: "a", projectId: "p1", ticketId: "t1", attachedTicketIds: ["t2"] });

  assert.equal(toProjectArtifactChange({ session: { id: "s1" } }), null);
  assert.equal(toProjectArtifactChange({ ticket: { id: "t1" }, deleted: true }), null);
  assert.equal(toProjectArtifactChange(null), null);
  assert.equal(toProjectArtifactChange("not json"), null);
});

test("project feed: an unreadable artifact change is BAD_RESPONSE", () => {
  assert.throws(() => toProjectArtifactChange({ artifact: { id: "", kind: "page" } }), Codes.BAD_RESPONSE);
  assert.throws(() => toProjectArtifactChange({ artifact: {}, deleted: true }), Codes.BAD_RESPONSE);
});
