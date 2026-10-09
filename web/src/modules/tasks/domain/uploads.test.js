import { Codes } from "../../../shared/domain/errors.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { MAX_RESOURCE_BYTES } from "./resources.js";
import { failed, newUpload, progressed, progressWords, retried, startable, started, uploadTitle, UploadState } from "./uploads.js";

file("tasks/domain/uploads");

const MB = 1024 * 1024;

test("a file over 25 MiB or an empty one fails before a byte is sent", () => {
  const big = newUpload("u1", { name: "take.mov", size: 32_925_696 });
  assert.equal(big.state, UploadState.FAILED);
  assert.deepEqual(big.failure, { message: "31.4 MB is over the 25 MiB limit.", code: "RESOURCE_TOO_LARGE", next: "compress it or add a link", retry: false });
  assert.equal(newUpload("u2", { name: "e.txt", size: 0 }).failure.message, "This file is empty.");
  assert.equal(newUpload("u3", { name: "max.bin", size: MAX_RESOURCE_BYTES }).state, UploadState.WAITING, "exactly 25 MiB is fine");
});

test("at most three run at once; the rest wait in order", () => {
  let rows = ["a", "b", "c", "d", "e"].map((k) => newUpload(k, { name: `${k}.png`, size: 10 }));
  assert.deepEqual(startable(rows).map((r) => r.key), ["a", "b", "c"]);
  rows = rows.map((r) => (["a", "b"].includes(r.key) ? started(r) : r));
  assert.deepEqual(startable(rows).map((r) => r.key), ["c"]);
});

test("the meta reads the progress, or the wait", () => {
  const row = newUpload("u1", { name: "deck.key", size: 23.9 * MB });
  assert.equal(progressWords(row), "Waiting · 23.9 MB");
  assert.equal(progressWords(progressed(started(row), 14.5 * MB)), "Uploading · 14.5 MB of 23.9 MB · 60%");
  assert.equal(progressWords(progressed(started(row), 99 * MB)), "Uploading · 23.9 MB of 23.9 MB · 100%", "progress never passes the size");
});

test("a dropped connection can be retried; a refusal cannot", () => {
  const row = started(newUpload("u1", { name: "a.png", size: 10 }));
  const dropped = failed(row, Codes.NETWORK);
  assert.deepEqual([dropped.failure.code, dropped.failure.retry, dropped.failure.message], ["network", true, "The upload stopped: the connection dropped."]);
  assert.equal(retried(dropped).state, UploadState.WAITING);
  assert.equal(failed(row, Codes.TICKET_NOT_FOUND).failure.retry, false);
  assert.equal(failed(row, Codes.INVALID_INPUT, "title too long").failure.message, "title too long");
});

test("a row is titled as the resource will be: the title sent, else the file name", () => {
  assert.equal(uploadTitle(newUpload("u1", { name: "image.png", size: 1 }, "Pasted image 09:05.png")), "Pasted image 09:05.png");
  assert.equal(uploadTitle(newUpload("u2", { name: "image.png", size: 1 })), "image.png");
});
