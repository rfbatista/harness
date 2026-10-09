import { Codes } from "../domain/errors.js";
import { fakeXHRClass } from "../testing/doubles.js";
import { assert, file, test } from "../testing/test.js";
import { xhrUpload } from "./upload.js";

file("shared/infrastructure/upload");

function subject() {
  const xhr = fakeXHRClass();
  return { upload: xhrUpload({ base: "/api", XMLHttpRequest: xhr.XMLHttpRequest }), xhr };
}

const blob = (text) => new Blob([text], { type: "text/plain" });

test("posts the parts in the order given and reports progress", async () => {
  const { upload, xhr } = subject();
  const seen = [];
  const answer = upload("/upload_ticket_resource", [["ticket_id", "t1"], ["title", "Notes"], ["file", blob("hello"), "notes.txt"]], {
    onProgress: (loaded, total) => seen.push([loaded, total]),
  });
  const req = xhr.latest();
  assert.deepEqual([req.method, req.url], ["POST", "/api/upload_ticket_resource"]);
  assert.deepEqual([...req.body.keys()], ["ticket_id", "title", "file"]);
  assert.equal(req.body.get("file").name, "notes.txt");
  req.progress(2, 5);
  req.progress(5, 5);
  req.respond(200, { resource: { id: "r1" } });
  assert.deepEqual(await answer, { resource: { id: "r1" } });
  assert.deepEqual(seen, [[2, 5], [5, 5]]);
});

test("a refusal carries the server's code and message", async () => {
  const { upload, xhr } = subject();
  const answer = upload("/upload_ticket_resource", [["file", blob("x"), "x"]]);
  xhr.latest().respond(413, { error: "over 25 MiB", code: "RESOURCE_TOO_LARGE" });
  const err = await assert.rejects(answer, Codes.RESOURCE_TOO_LARGE);
  assert.deepEqual([err.message, err.status], ["over 25 MiB", 413]);
});

test("a 413 without our JSON is still RESOURCE_TOO_LARGE; other garbage is BAD_RESPONSE", async () => {
  const { upload, xhr } = subject();
  const a = upload("/u", []);
  xhr.latest().respond(413, "Request Entity Too Large");
  await assert.rejects(a, Codes.RESOURCE_TOO_LARGE);
  const b = upload("/u", []);
  xhr.latest().respond(200, "<html>");
  await assert.rejects(b, Codes.BAD_RESPONSE);
});

test("a dropped connection is NETWORK", async () => {
  const { upload, xhr } = subject();
  const answer = upload("/u", []);
  xhr.latest().fail();
  await assert.rejects(answer, Codes.NETWORK);
});

test("aborting the signal cancels the request with an AbortError", async () => {
  const { upload, xhr } = subject();
  const ctrl = new AbortController();
  const answer = upload("/u", [], { signal: ctrl.signal });
  ctrl.abort();
  const err = await assert.rejects(answer);
  assert.equal(err.name, "AbortError");
  assert.ok(xhr.latest().aborted);

  const already = new AbortController();
  already.abort();
  const late = await assert.rejects(upload("/u", [], { signal: already.signal }));
  assert.equal(late.name, "AbortError");
});
