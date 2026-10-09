// The ResourceGateway contract (../domain/ports.js), run by every implementation.

import { Codes } from "../../../shared/domain/errors.js";
import { flush } from "../../../shared/testing/doubles.js";
import { assert, test } from "../../../shared/testing/test.js";
import { MAX_RESOURCE_BYTES } from "../domain/resources.js";
import { makeComment, makeResource, resourceDTO } from "./resource-fixtures.js";

/**
 * @typedef {object} Subject
 * @property {import("../domain/ports.js").ResourceGateway} gateway
 * @property {(ticketId: string) => void} deleteTask  the task is deleted elsewhere
 *
 * @param {string} name
 * @param {(world: Parameters<typeof import("../infrastructure/memory-resources.js").memoryResources>[0]) => Subject} makeSubject
 */
export function resourceGatewayContract(name, makeSubject) {
  const contract = (title, fn) => test(`${name} · ${title}`, fn);
  const world = (more = {}) => ({ tickets: [{ id: "t1", projectId: "p1" }, { id: "t2", projectId: "p1" }], ...more });
  const fileOf = (text, filename, type = "") => new File([text], filename, { type });

  contract("an upload becomes a resource, its kind from the MIME type and its title from the file name", async () => {
    const { gateway } = makeSubject(world());
    const png = await gateway.upload({ ticketId: "t1", file: fileOf("png!", "wireframe.png", "image/png") });
    assert.deepEqual([png.kind, png.title, png.mime, png.sizeBytes, png.ticketId, png.url], ["image", "wireframe.png", "image/png", 4, "t1", ""]);
    assert.equal(png.contentUrl, `/api/ticket_resources/${png.id}/content`);
    const audio = await gateway.upload({ ticketId: "t1", file: fileOf("abc", "standup.m4a", "audio/mp4"), title: "  Stand-up  " });
    assert.deepEqual([audio.kind, audio.title], ["audio", "Stand-up"]);
    const svg = await gateway.upload({ ticketId: "t1", file: fileOf("<svg/>", "logo.svg", "image/svg+xml") });
    assert.equal(svg.kind, "image", "SVG is an image; the UI gives it no preview");
    const pdf = await gateway.upload({ ticketId: "t1", file: fileOf("%PDF", "spec.pdf", "application/octet-stream") });
    assert.deepEqual([pdf.kind, pdf.mime], ["file", "application/pdf"], "octet-stream falls back to the extension");
    assert.deepEqual((await gateway.listResources("t1")).map((r) => r.id), [png.id, audio.id, svg.id, pdf.id], "oldest first");
    assert.deepEqual(await gateway.listResources("t2"), []);
  });

  contract("an upload reports its progress, up to the file's size", async () => {
    const { gateway } = makeSubject(world());
    const seen = [];
    await gateway.upload({ ticketId: "t1", file: fileOf("hello", "a.txt", "text/plain"), onProgress: (loaded, total) => seen.push([loaded, total]) });
    assert.ok(seen.length > 0, "at least one progress report");
    assert.equal(seen.at(-1)[0], seen.at(-1)[1], "the last report is the whole file");
  });

  contract("an upload is refused for a missing task, an empty file or one over 25 MiB", async () => {
    const { gateway } = makeSubject(world());
    await assert.rejects(gateway.upload({ ticketId: "ghost", file: fileOf("x", "x.txt") }), Codes.TICKET_NOT_FOUND);
    await assert.rejects(gateway.upload({ ticketId: "t1", file: fileOf("", "empty.txt") }), Codes.INVALID_INPUT);
    const big = new File([new Uint8Array(MAX_RESOURCE_BYTES + 1)], "take.mov", { type: "video/quicktime" });
    await assert.rejects(gateway.upload({ ticketId: "t1", file: big }), Codes.RESOURCE_TOO_LARGE);
    assert.deepEqual(await gateway.listResources("t1"), []);
  });

  contract("a cancelled upload rejects with an AbortError", async () => {
    const { gateway } = makeSubject(world());
    const ctrl = new AbortController();
    ctrl.abort();
    const err = await assert.rejects(gateway.upload({ ticketId: "t1", file: fileOf("x", "x.txt"), signal: ctrl.signal }));
    assert.equal(err.name, "AbortError");
  });

  contract("a link's title defaults to its address; only http(s) is accepted", async () => {
    const { gateway } = makeSubject(world());
    const link = await gateway.addLink({ ticketId: "t1", url: " https://figma.com/file/abc " });
    assert.deepEqual([link.kind, link.url, link.title, link.contentUrl, link.mime, link.sizeBytes], ["link", "https://figma.com/file/abc", "https://figma.com/file/abc", "", "", 0]);
    assert.equal((await gateway.addLink({ ticketId: "t1", url: "http://localhost:3000", title: "Dev server" })).title, "Dev server");
    await assert.rejects(gateway.addLink({ ticketId: "t1", url: "ftp://example.com" }), Codes.INVALID_INPUT);
    await assert.rejects(gateway.addLink({ ticketId: "ghost", url: "https://example.com" }), Codes.TICKET_NOT_FOUND);
  });

  contract("retitle trims; a blank title or a missing resource is refused", async () => {
    const { gateway } = makeSubject(world({ resources: [makeResource({ id: "r1" })] }));
    const r = await gateway.retitle("r1", "  final.png ");
    assert.equal(r.title, "final.png");
    assert.ok(r.updatedAt.getTime() > makeResource().updatedAt.getTime(), "a retitle is a new updated_at");
    await assert.rejects(gateway.retitle("r1", "   "), Codes.INVALID_INPUT);
    await assert.rejects(gateway.retitle("ghost", "x"), Codes.RESOURCE_NOT_FOUND);
  });

  contract("removeResource removes it; twice is RESOURCE_NOT_FOUND", async () => {
    const { gateway } = makeSubject(world({ resources: [makeResource({ id: "r1" })] }));
    await gateway.removeResource("r1");
    assert.deepEqual(await gateway.listResources("t1"), []);
    await assert.rejects(gateway.removeResource("r1"), Codes.RESOURCE_NOT_FOUND);
  });

  contract("comments: added trimmed, oldest first; refused empty, over 10,000 or on a missing task", async () => {
    const { gateway } = makeSubject(world());
    const a = await gateway.addComment("t1", "  Keep the v2 contract.\n\n- one\n- two  ");
    assert.deepEqual([a.body, a.author, a.ticketId], ["Keep the v2 contract.\n\n- one\n- two", "person", "t1"]);
    const b = await gateway.addComment("t1", "second");
    assert.deepEqual((await gateway.listComments("t1")).map((c) => c.id), [a.id, b.id]);
    await assert.rejects(gateway.addComment("t1", "  "), Codes.INVALID_INPUT);
    await assert.rejects(gateway.addComment("t1", "x".repeat(10_001)), Codes.INVALID_INPUT);
    await assert.rejects(gateway.addComment("ghost", "x"), Codes.TICKET_NOT_FOUND);
  });

  contract("an edit is a new updated_at; one that changes nothing keeps it", async () => {
    const { gateway } = makeSubject(world({ comments: [makeComment({ id: "c1", body: "old" })] }));
    const same = await gateway.editComment("c1", " old ");
    assert.equal(same.updatedAt.getTime(), same.createdAt.getTime());
    const edited = await gateway.editComment("c1", "new");
    assert.ok(edited.updatedAt.getTime() > edited.createdAt.getTime());
    await assert.rejects(gateway.editComment("c1", ""), Codes.INVALID_INPUT);
    await assert.rejects(gateway.editComment("ghost", "x"), Codes.COMMENT_NOT_FOUND);
  });

  contract("removeComment removes it; twice is COMMENT_NOT_FOUND", async () => {
    const { gateway } = makeSubject(world({ comments: [makeComment({ id: "c1" })] }));
    await gateway.removeComment("c1");
    assert.deepEqual(await gateway.listComments("t1"), []);
    await assert.rejects(gateway.removeComment("c1"), Codes.COMMENT_NOT_FOUND);
  });

  contract("follow delivers every change of the project's tasks, in order, and nothing else", async () => {
    const subject = makeSubject(world({ tickets: [{ id: "t1", projectId: "p1" }, { id: "t9", projectId: "p9" }], resources: [makeResource({ id: "r1" })] }));
    const { gateway } = subject;
    const changes = [];
    const statuses = [];
    const stop = gateway.follow("p1", (c) => changes.push(c), (s) => statuses.push(s));
    await flush();
    const link = await gateway.addLink({ ticketId: "t1", url: "https://example.com" });
    await gateway.retitle(link.id, "Example");
    await gateway.removeResource("r1");
    const c = await gateway.addComment("t1", "hi");
    await gateway.editComment(c.id, "hello");
    await gateway.removeComment(c.id);
    await gateway.addComment("t9", "another project");
    await flush();
    assert.deepEqual(
      changes.map((x) => [x.kind, x.resource?.title ?? x.comment?.body ?? x.id]),
      [["resource", "https://example.com"], ["resource", "Example"], ["resource-deleted", "r1"], ["comment", "hi"], ["comment", "hello"], ["comment-deleted", c.id]],
    );
    assert.equal(changes[2].ticketId, "t1", "a delete names its task");
    assert.ok(statuses.includes("live"));
    stop();
    await gateway.addComment("t1", "after unfollow");
    await flush();
    assert.equal(changes.length, 6);
  });

  contract("a deleted task is one change, not one per resource or comment", async () => {
    const subject = makeSubject(world({ resources: [makeResource({ id: "r1" })], comments: [makeComment({ id: "c1" })] }));
    const changes = [];
    const stop = subject.gateway.follow("p1", (c) => changes.push(c), () => {});
    await flush();
    subject.deleteTask("t1");
    await flush();
    assert.deepEqual(changes, [{ kind: "task-deleted", id: "t1" }]);
    stop();
  });

  contract("decodeSeed reads the API's shapes and rejects garbage", () => {
    const { gateway } = makeSubject(world());
    const seed = gateway.decodeSeed({ project_id: "p1", ticket_id: "t1", resources: [resourceDTO(makeResource())], comments: [] });
    assert.deepEqual([seed.ticketId, seed.resources[0].title, seed.resources[0].createdAt instanceof Date, seed.comments.length], ["t1", "wireframe.png", true, 0]);
    assert.equal(gateway.decodeSeed({ ticket_id: "t1" }).resources.length, 0, "missing lists are empty");
    assert.throws(() => gateway.decodeSeed({ ticket_id: "t1", resources: [{ id: "r1", kind: "sticker" }] }), Codes.BAD_RESPONSE);
    assert.throws(() => gateway.decodeSeed(null), Codes.BAD_RESPONSE);
  });
}
