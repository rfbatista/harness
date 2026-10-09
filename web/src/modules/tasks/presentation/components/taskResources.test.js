import { Codes } from "../../../../shared/domain/errors.js";
import { FeedStatus } from "../../../../shared/domain/feed.js";
import { fixedClock } from "../../../../shared/infrastructure/clock.js";
import { mount, seededElement } from "../../../../shared/testing/alpine.js";
import { flush, manualTimers } from "../../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { MAX_RESOURCE_BYTES } from "../../domain/resources.js";
import { memoryResources } from "../../infrastructure/memory-resources.js";
import { commentDTO, everyKind, makeComment, makeResource, resourceDTO, sessionComment } from "../../testing/resource-fixtures.js";
import { taskResources, TOAST_MS } from "./taskResources.js";

file("tasks/presentation/components/taskResources");

const NOW = "2026-10-08T15:00:00Z";

function page({ resources = [], comments = [], sessionIds = [], holdUploads = false, tickets = [{ id: "t1", projectId: "p1" }, { id: "t2", projectId: "p1" }] } = {}) {
  const sessions = sessionIds.map((id) => ({ id, ticketId: "t1" }));
  const memory = memoryResources({ tickets, resources, comments, sessions, holdUploads, now: () => new Date(NOW) });
  const timers = manualTimers();
  const copied = [];
  const el = seededElement({ project_id: "p1", ticket_id: "t1", resources: resources.filter((r) => r.ticketId === "t1").map(resourceDTO), comments: comments.map(commentDTO), session_ids: sessionIds }, "task-resources-seed");
  const m = mount(
    taskResources({
      resources: memory.gateway,
      clock: fixedClock(NOW),
      copy: async (text) => void copied.push(text),
      setTimeout: timers.setTimeout,
      setInterval: () => 1,
      clearInterval: () => {},
    }),
    { el },
  );
  m.instance.init();
  return { ...m, page: m.instance, memory, timers, copied };
}

const fileOf = (name, size = 5, type = "") => new File([new Uint8Array(size)], name, { type });
const titles = (p) => p.rows.map((r) => r.title);

test("the page starts from the seed: rows of every kind, the counts and the summary", () => {
  const { page: p } = page({ resources: everyKind(), comments: [makeComment()] });
  assert.deepEqual(titles(p), ["wireframe.png", "logo.svg", "contract-v2.pdf", "notes.txt", "session-export.json", "standup.m4a", "Figma file"]);
  assert.equal(p.summary, "7 resources · 1 comment");
  assert.deepEqual([p.resourceCountLabel, p.commentCountLabel], ["7 resources", "1 comment"]);
  const [png, svg, pdf] = p.rows;
  assert.deepEqual([png.isLightbox, png.thumb !== "", svg.isLightbox, svg.download, pdf.newTab], [true, true, false, true, true]);
  assert.ok(png.meta.startsWith("PNG image · 403 KB · "));
  assert.equal(p.rows.at(-1).host, "figma.com/file/abc");
});

test("what another tab adds arrives at the end with the arrival wash; other tasks' changes do not", async () => {
  const { page: p, memory } = page({ resources: [makeResource({ id: "r1" })] });
  await memory.gateway.addLink({ ticketId: "t1", url: "https://example.com" });
  await memory.gateway.addLink({ ticketId: "t2", url: "https://other.example" });
  await memory.gateway.addComment("t1", "from the other tab");
  assert.deepEqual(titles(p), ["wireframe.png", "https://example.com"]);
  assert.ok(p.rows[1].fresh && !p.rows[0].fresh);
  assert.deepEqual(p.commentRows.map((c) => [c.body, c.fresh]), [["from the other tab", true]]);
});

test("an upload is a row at once, shows its progress, then becomes the resource exactly once", async () => {
  const { page: p, memory, timers } = page({ holdUploads: true });
  p.addFiles([fileOf("wireframe.png", 2048, "image/png")]);
  await flush();
  assert.deepEqual(p.uploadRows.map((u) => [u.title, u.state]), [["wireframe.png", "uploading"]]);
  assert.equal(p.announcement, "Uploading wireframe.png.");
  assert.equal(p.resourceCount, 0, "an upload in flight does not count");

  memory.uploads[0].progress(1024);
  assert.equal(p.uploadRows[0].meta, "Uploading · 1 KB of 2 KB · 50%");
  memory.uploads[0].finish(); // the feed echo goes out before the answer
  await flush();
  assert.deepEqual(titles(p), ["wireframe.png"], "the echo was held, the answer shown: once");
  assert.deepEqual(p.uploadRows, []);
  assert.ok(p.rows[0].fresh);
  assert.deepEqual(p.toasts.map((t) => t.message), ["Added “wireframe.png”. Sessions running on this task are told about it."]);
  timers.tick();
  assert.deepEqual(p.toasts, [], `a toast goes after ${TOAST_MS} ms`);
});

test("three uploads run at once; the fourth waits for a slot", async () => {
  const { page: p, memory } = page({ holdUploads: true });
  p.addFiles(["a", "b", "c", "d"].map((n) => fileOf(`${n}.txt`)));
  await flush();
  assert.deepEqual(p.uploadRows.map((u) => u.state), ["uploading", "uploading", "uploading", "waiting"]);
  assert.equal(p.uploadRows[3].meta, "Waiting · 5 B");
  assert.equal(p.announcement, "Uploading 4 files.");
  memory.uploads[0].finish();
  await flush();
  await flush();
  assert.deepEqual(p.uploadRows.map((u) => [u.title, u.state]), [["b.txt", "uploading"], ["c.txt", "uploading"], ["d.txt", "uploading"]]);
});

test("a file over 25 MiB fails before sending; Dismiss removes it", async () => {
  const { page: p, memory } = page({ holdUploads: true });
  const big = { name: "take.mov", size: MAX_RESOURCE_BYTES + 1, type: "video/quicktime" };
  p.addFiles([big]);
  await flush();
  assert.equal(memory.uploads.length, 0, "nothing was sent");
  const [row] = p.uploadRows;
  assert.deepEqual([row.failed, row.meta, row.code, row.canRetry], [true, "25.0 MB is over the 25 MiB limit.", "RESOURCE_TOO_LARGE · compress it or add a link", false]);
  p.dismissUpload(row.key);
  assert.deepEqual(p.uploadRows, []);
});

test("a dropped connection fails the row with Retry; a retry sends it again", async () => {
  const { page: p, memory } = page({ holdUploads: true });
  p.addFiles([fileOf("notes.txt")]);
  await flush();
  memory.uploads[0].fail(Codes.NETWORK);
  await flush();
  const [row] = p.uploadRows;
  assert.deepEqual([row.failed, row.meta, row.code, row.canRetry], [true, "The upload stopped: the connection dropped.", "network · try again", true]);
  p.retryUpload(row.key);
  await flush();
  assert.equal(p.uploadRows[0].state, "uploading");
  memory.uploads[1].finish();
  await flush();
  assert.deepEqual(titles(p), ["notes.txt"]);
});

test("Cancel aborts a running upload and removes its row", async () => {
  const { page: p, memory } = page({ holdUploads: true });
  p.addFiles([fileOf("big.bin", 50)]);
  await flush();
  p.cancelUpload(p.uploadRows[0].key);
  await flush();
  assert.deepEqual(p.uploadRows, []);
  assert.equal(memory.uploads.length, 0);
  assert.deepEqual(memory.resources(), []);
});

test("a pasted screenshot is titled by the time; pasting into a field is left alone", async () => {
  const { page: p, memory } = page({ holdUploads: true });
  let prevented = false;
  const shot = fileOf("image.png", 10, "image/png");
  p.pasted({ target: document.body, clipboardData: { files: [shot] }, preventDefault: () => (prevented = true) });
  await flush();
  assert.ok(prevented);
  assert.ok(/^Pasted image \d\d:\d\d\.png$/.test(p.uploadRows[0].title));
  assert.equal(memory.uploads[0].title, p.uploadRows[0].title, "the title is sent with the file");
  p.pasted({ target: document.createElement("textarea"), clipboardData: { files: [shot] }, preventDefault: () => {} });
  assert.equal(p.uploadRows.length, 1);
});

test("dragging files arms the column; over it, the overlay; a drop adds them", async () => {
  const { page: p } = page({ holdUploads: true });
  const files = [fileOf("a.png", 5, "image/png")];
  const drag = { dataTransfer: { types: ["Files"], files }, preventDefault() {} };
  p.windowDragEnter(drag);
  assert.ok(p.dropArmed && !p.dropOver);
  p.zoneDragEnter(drag);
  p.zoneDragEnter(drag); // a child of the column
  p.zoneDragLeave(drag);
  assert.ok(p.dropOver, "leaving a child is not leaving the column");
  p.zoneDrop(drag);
  await flush();
  assert.ok(!p.dropArmed && !p.dropOver);
  assert.deepEqual(p.uploadRows.map((u) => u.title), ["a.png"]);
  p.windowDragEnter({ dataTransfer: { types: ["text/plain"] } });
  assert.ok(!p.dropArmed, "dragging text does not arm it");
});

test("a link is checked before sending; once added it shows once and the form closes", async () => {
  const { page: p } = page();
  p.openLink();
  p.linkUrl = "example.com";
  await p.submitLink();
  assert.equal(p.linkError, "Use an address that starts with http:// or https://.");
  assert.ok(p.linkInvalid && p.linkOpen);
  p.linkUrl = "https://figma.com/file/abc";
  p.linkTitle = "Figma";
  await p.submitLink();
  assert.deepEqual(titles(p), ["Figma"], "the echo before the answer was held");
  assert.ok(!p.linkOpen);
  assert.deepEqual([p.linkUrl, p.linkError], ["", ""]);
});

test("rename saves in place; a blank title is refused; a resource deleted elsewhere is dropped", async () => {
  const { page: p, memory } = page({ resources: [makeResource({ id: "r1" }), makeResource({ id: "r2", title: "b.png" })] });
  p.startRename("r1");
  assert.ok(p.rows[0].renaming);
  p.renameText = "   ";
  await p.saveRename();
  assert.equal(p.renameError, "A resource needs a title.");
  p.renameText = " final.png ";
  await p.saveRename();
  assert.deepEqual([titles(p)[0], p.renamingId, p.announcement], ["final.png", "", "Renamed to “final.png”."]);

  p.startRename("r2");
  await memory.gateway.removeResource("r2");
  assert.deepEqual(titles(p), ["final.png"]);
  assert.equal(p.renamingId, "", "the rename closes");
  assert.deepEqual(p.toasts.map((t) => t.message), ["“b.png” was deleted elsewhere."]);
});

test("delete asks in the row first; a 404 counts as done", async () => {
  const { page: p, memory } = page({ resources: [makeResource({ id: "r1" }), makeResource({ id: "r2", title: "b.png" })] });
  p.askDelete("r1");
  assert.ok(p.rows[0].confirming);
  p.keep();
  assert.equal(p.confirmingId, "");
  p.askDelete("r1");
  await p.confirmDelete();
  assert.deepEqual(titles(p), ["b.png"]);
  assert.deepEqual(p.toasts.map((t) => t.message), ["Deleted “wireframe.png”."]);

  // Gone on the server already, and the feed has not said so yet.
  p.resources = [...p.resources, makeResource({ id: "ghost", title: "ghost.png" })];
  p.askDelete("ghost");
  await p.confirmDelete();
  assert.deepEqual(titles(p), ["b.png"], "RESOURCE_NOT_FOUND is what the person asked for");
});

test("the row menu runs view, copy, rename and delete for its row", async () => {
  const { page: p, copied } = page({ resources: [makeResource({ id: "r1", kind: "link", title: "Docs", url: "https://docs.example", mime: "", sizeBytes: 0, contentUrl: "" })] });
  const row = document.createElement("li");
  row.dataset.id = "r1";
  const item = document.createElement("button");
  row.append(item);
  p.rowAction({ target: item, detail: { action: "copy-address" } });
  await flush();
  assert.deepEqual(copied, ["https://docs.example"]);
  assert.deepEqual(p.toasts.map((t) => t.message), ["Address copied."]);
  p.rowAction({ target: item, detail: { action: "rename" } });
  assert.equal(p.renamingId, "r1");
  p.rowAction({ target: item, detail: { action: "delete" } });
  assert.deepEqual([p.renamingId, p.confirmingId], ["", "r1"]);
});

test("comments: send clears the draft and shows the comment once; Ctrl Enter on an empty draft sends nothing", async () => {
  const { page: p, memory } = page();
  assert.ok(p.hasNoComments && p.cannotSend);
  await p.send();
  assert.deepEqual(memory.comments(), []);
  p.draft = "  Keep the v2 contract.  ";
  assert.ok(!p.cannotSend);
  await p.send();
  assert.deepEqual(p.commentRows.map((c) => c.body), ["Keep the v2 contract."], "the echo before the answer was held");
  assert.equal(p.draft, "");
  assert.equal(p.announcement, "Comment added. Sessions running on this task get it as a message.");
});

test("the composer counts runes from 9,000, amber then red past the limit", () => {
  const { page: p } = page();
  p.draft = "x".repeat(8_999);
  assert.ok(!p.showsCounter);
  p.draft = "x".repeat(9_412);
  assert.deepEqual([p.showsCounter, p.counter, p.counterTone], [true, "9,412 / 10,000", "attention"]);
  p.draft = "😀".repeat(10_000);
  assert.deepEqual([p.counterTone, p.cannotSend], ["attention", false], "10,000 emoji are 10,000 runes: sendable");
  p.draft = "x".repeat(10_001);
  assert.deepEqual([p.counterTone, p.cannotSend], ["danger", true]);
});

test("editing a comment: an unchanged text sends nothing; a change saves and shows edited", async () => {
  const created = new Date("2026-10-08T12:00:00Z");
  const { page: p } = page({ comments: [makeComment({ id: "c1", body: "old", createdAt: created, updatedAt: created })] });
  p.startEdit("c1");
  p.editText = " old ";
  await p.saveEdit();
  assert.deepEqual([p.editingId, p.commentRows[0].edited], ["", false]);
  p.startEdit("c1");
  p.editText = "new";
  await p.saveEdit();
  assert.deepEqual([p.commentRows[0].body, p.commentRows[0].edited, p.editingId], ["new", true, ""]);
});

test("deleting a comment asks first; one deleted elsewhere while editing closes the edit", async () => {
  const { page: p, memory } = page({ comments: [makeComment({ id: "c1" }), makeComment({ id: "c2", body: "two" })] });
  p.askDeleteComment("c1");
  assert.ok(p.commentRows[0].confirming);
  await p.confirmDeleteComment();
  assert.deepEqual(p.commentRows.map((c) => c.id), ["c2"]);
  assert.equal(p.announcement, "Comment deleted.");
  p.startEdit("c2");
  await memory.gateway.removeComment("c2");
  assert.deepEqual([p.comments.length, p.editingId], [0, ""]);
  assert.deepEqual(p.toasts.map((t) => t.message), ["That comment was deleted elsewhere."]);
});

test("the task deleted elsewhere: the page says so and adding stops", async () => {
  const { page: p, memory } = page({ holdUploads: true });
  p.addFiles([fileOf("a.txt")]);
  await flush();
  memory.deleteTask("t1");
  await flush();
  assert.ok(p.taskGone && p.cannotAdd && p.cannotSend);
  assert.deepEqual(p.uploadRows, [], "uploads in flight are cancelled");
  p.openLink();
  assert.ok(!p.linkOpen);
});

test("after the feed resyncs both lists are read again", async () => {
  const { page: p, memory } = page({ resources: [makeResource({ id: "r1" })] });
  memory.feedStatus(FeedStatus.PAUSED);
  p.resources = []; // stale while the feed was down
  memory.feedStatus(FeedStatus.RESYNCED);
  await flush();
  assert.deepEqual(titles(p), ["wireframe.png"]);
});

test("the lightbox steps through the images the browser can show, and closes when its image is deleted", async () => {
  const { page: p, memory } = page({ resources: everyKind() });
  p.openLightbox("r-svg");
  assert.equal(p.lightboxId, "", "SVG has no preview");
  p.openLightbox("r-png");
  assert.deepEqual([p.lightboxTitle, p.lightboxMeta, p.lightboxSteps], ["wireframe.png", "PNG image · 403 KB · 1 of 1", false]);
  p.nextImage();
  assert.equal(p.lightboxId, "r-png", "one image: stepping stays");
  await memory.gateway.removeResource("r-png");
  assert.equal(p.lightboxId, "");
});

test("a change the feed delivers while a resync's lists are on their way is not lost", async () => {
  const { page: p, memory } = page({ resources: [makeResource({ id: "r1" }), makeResource({ id: "r2", title: "b.png" })] });
  memory.feedStatus(FeedStatus.RESYNCED); // the lists are asked for…
  await memory.gateway.removeResource("r2"); // …and r2 is deleted before they answer
  await memory.gateway.addComment("t1", "meanwhile");
  await flush();
  await flush();
  assert.deepEqual(titles(p), ["wireframe.png"]);
  assert.deepEqual(p.commentRows.map((c) => c.body), ["meanwhile"]);
});

test("the task deleted elsewhere closes an edit, a delete question and the lightbox; Retry is gone", async () => {
  const { page: p, memory } = page({ resources: everyKind(), comments: [makeComment({ id: "c1" })], holdUploads: true });
  p.addFiles([fileOf("a.txt")]);
  await flush();
  memory.uploads[0].fail(Codes.NETWORK);
  await flush();
  p.startEdit("c1");
  p.openLightbox("r-png");
  memory.deleteTask("t1");
  assert.deepEqual([p.editingId, p.confirmingCommentId, p.lightboxId], ["", "", ""]);
  assert.equal(p.uploadRows[0].canRetry, false);
  p.askDeleteComment("c1");
  assert.equal(p.confirmingCommentId, "");
});

test("a file name longer than the title limit still matches its echo: nothing shows twice", async () => {
  const { page: p, memory } = page({ holdUploads: true });
  const long = `${"n".repeat(230)}.png`;
  p.addFiles([fileOf(long, 5, "image/png")]);
  await flush();
  memory.uploads[0].finish();
  await flush();
  assert.equal(p.rows.length, 1);
  assert.equal(p.rows[0].title.length, 200);
});

test("a session's comment names its agent, links to its session while it is listed, and has no Edit", () => {
  const { page: p } = page({
    comments: [makeComment({ id: "c1" }), sessionComment({ id: "c2" }), sessionComment({ id: "c3", sessionId: "s-gone", agentName: "" })],
    sessionIds: ["s-rev"],
  });
  assert.deepEqual(
    p.commentRows.map((c) => [c.author, c.bySession, c.authorHref, c.canEdit]),
    [
      ["You", false, "", true],
      ["Reviewer", true, "/projects/p1/tasks/t1?session=s-rev", false],
      ["Agent session", true, "", false],
    ],
  );
  assert.equal(p.commentRows[1].deleteLabel, "Delete comment by Reviewer from 3 h ago");
  p.startEdit("c2");
  assert.equal(p.editingId, "", "a session's comment cannot be edited here");
});

test("the person deletes a session's comment", async () => {
  const { page: p, memory } = page({ comments: [sessionComment({ id: "c2" })], sessionIds: ["s-rev"] });
  p.askDeleteComment("c2");
  await p.confirmDeleteComment();
  assert.deepEqual([p.comments, memory.comments()], [[], []]);
  assert.equal(p.announcement, "Comment deleted.");
});

test("a stale edit of a session's comment is refused: the edit closes and the comment stays", async () => {
  // A page that took the comment for the person's (from before sessions wrote comments).
  const { page: p, memory } = page({ comments: [sessionComment({ id: "c2", body: "theirs" })] });
  p.comments = [makeComment({ id: "c2", body: "theirs" })];
  p.startEdit("c2");
  p.editText = "mine now";
  await p.saveEdit();
  assert.deepEqual([p.editingId, p.editError, p.comments.length], ["", "", 1]);
  assert.deepEqual(p.toasts.map((t) => t.message), ["Only the session that wrote this comment can edit it."]);
  assert.equal(memory.comments()[0].body, "theirs");
});

test("what a session writes arrives live, in order: added, edited and deleted, announced once", async () => {
  const { page: p, memory } = page({ comments: [makeComment({ id: "c1" })], sessionIds: ["s-rev"] });
  const c = memory.sessionAdds({ ticketId: "t1", sessionId: "s-rev", agentName: "Reviewer", body: "Found it." });
  assert.deepEqual(p.commentRows.map((r) => [r.author, r.body, r.fresh]), [["You", "Keep the v2 contract.", false], ["Reviewer", "Found it.", true]]);
  assert.equal(p.announcement, "Reviewer commented.");
  memory.sessionEdits(c.id, "Found it twice.");
  assert.deepEqual(p.commentRows.map((r) => r.body), ["Keep the v2 contract.", "Found it twice."]);
  assert.ok(p.commentRows[1].edited);
  assert.equal(p.announcement, "Reviewer commented.", "an edit is not announced");
  memory.sessionAdds({ ticketId: "t2", sessionId: "s-other", body: "another task" });
  memory.sessionDeletes(c.id);
  assert.deepEqual(p.commentRows.map((r) => r.id), ["c1"]);
});

test("a session's comment with the body of the person's pending one is not taken for its echo", async () => {
  const { page: p, memory } = page();
  p.draft = "Same words.";
  const sending = p.send();
  memory.sessionAdds({ ticketId: "t1", sessionId: "s-rev", agentName: "Reviewer", body: "Same words." });
  assert.deepEqual(p.commentRows.map((r) => r.author), ["Reviewer"], "shown at once, not held");
  await sending;
  assert.deepEqual(p.commentRows.map((r) => r.author), ["Reviewer", "You"]);
});

test("a session starting on the task links its comments; deleted, it no longer does", async () => {
  const { page: p, memory } = page({ comments: [sessionComment({ id: "c2" })] });
  assert.equal(p.commentRows[0].authorHref, "");
  memory.startSession("t2", "s-rev"); // another task's session is not this task's
  assert.equal(p.commentRows[0].authorHref, "");
  memory.startSession("t1", "s-rev");
  assert.equal(p.commentRows[0].authorHref, "/projects/p1/tasks/t1?session=s-rev");
  memory.deleteSession("s-rev");
  assert.equal(p.commentRows[0].authorHref, "");
});
