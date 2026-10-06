import { assert, file, test } from "../../../shared/testing/test.js";
import { A0, makeArtifact } from "../testing/artifact-fixtures.js";
import { artifactTitle, kindWord, toCardView, toPreviewView } from "./artifactView.js";

file("sessions/presentation/artifactView");

const now = new Date(A0.getTime() + 4 * 60_000);

test("a title falls back to the file name, then the url, then Untitled", () => {
  assert.equal(artifactTitle(makeArtifact({ title: "Hero" })), "Hero");
  assert.equal(artifactTitle(makeArtifact({ title: "", path: "shots/hero.png" })), "hero.png");
  assert.equal(artifactTitle(makeArtifact({ title: "", kind: "url", path: "", url: "http://localhost:3000/" })), "http://localhost:3000/");
  assert.equal(artifactTitle(makeArtifact({ title: "", path: "" })), "Untitled artifact");
});

test("kinds read as words; url is a dev server", () => {
  assert.deepEqual(["page", "image", "video", "url", "file"].map(kindWord), ["page", "image", "video", "dev server", "file"]);
});

test("a card carries title, kind, note, revision, time, selection and freshness", () => {
  const card = toCardView(makeArtifact({ id: "a1", revision: 2, note: "tighter" }), { selectedId: "a1", now, fresh: true });
  assert.deepEqual(card, { id: "a1", title: "Pricing card", kindWord: "page", note: "tighter", revision: "rev 2", updated: "4m", selected: true, fresh: true });
});

test("a page preview is a sandboxed frame keyed on its revision, reloaded per revision", () => {
  const p = toPreviewView(makeArtifact({ id: "a1", revision: 3 }), now);
  assert.equal(p.key, "a1@3");
  assert.equal(p.src, "/api/artifacts/a1/view/?rev=3");
  assert.equal(p.openHref, "/api/artifacts/a1/view/");
  assert.deepEqual([p.isPage, p.isImage, p.isVideo, p.isFile, p.isUrl, p.embed, p.notEmbeddable], [true, false, false, false, false, true, false]);
  assert.equal(p.embedsUrl, false);
  assert.equal(p.frameTitle, "Pricing card, revision 3");
});

test("an image, a video and a file carry what their elements need", () => {
  const img = toPreviewView(makeArtifact({ kind: "image", path: "hero.png", mime: "image/png", sizeBytes: 1536 }), now);
  assert.ok(img.isImage && img.src.endsWith("/view/?rev=1"));
  const vid = toPreviewView(makeArtifact({ kind: "video", path: "demo.mp4", mime: "video/mp4" }), now);
  assert.ok(vid.isVideo);
  const f = toPreviewView(makeArtifact({ kind: "file", title: "", path: "out/report.pdf", mime: "application/pdf", sizeBytes: 2.5 * 1024 * 1024 }), now);
  assert.deepEqual([f.isFile, f.fileName, f.size, f.mime, f.title], [true, "report.pdf", "2.5 MB", "application/pdf", "report.pdf"]);
  assert.equal(f.openHref, "/api/artifacts/a1/view/");
});

test("a loopback url is embedded as is; any other url is shown, not embedded", () => {
  const ok = toPreviewView(makeArtifact({ kind: "url", path: "", url: "http://localhost:5173/", revision: 2 }), now);
  assert.deepEqual([ok.isUrl, ok.embed, ok.notEmbeddable, ok.src, ok.openHref, ok.key], [true, true, false, "http://localhost:5173/", "http://localhost:5173/", "a1@2"]);
  assert.equal(ok.embedsUrl, true);
  const no = toPreviewView(makeArtifact({ kind: "url", path: "", url: "http://example.com/" }), now);
  assert.deepEqual([no.isUrl, no.embed, no.notEmbeddable, no.src], [true, false, true, ""]);
  assert.equal(no.embedsUrl, false);
  assert.equal(no.openHref, "", "an off-machine url is not even linked");
});
