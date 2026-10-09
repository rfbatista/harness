import { assert, file, test } from "../../../shared/testing/test.js";
import { makeResource } from "../testing/resource-fixtures.js";
import {
  addressWords,
  downloadHref,
  extensionLabel,
  linkProblem,
  MAX_RESOURCE_BYTES,
  menuItems,
  pastedTitle,
  sizeWords,
  tileKind,
  titleAction,
  titleHref,
  titleProblem,
  tooLarge,
  typeWord,
  viewableImages,
} from "./resources.js";

file("tasks/domain/resources");

const png = makeResource({ id: "r1", kind: "image", title: "wireframe.png", mime: "image/png", sizeBytes: 412_672 });
const svg = makeResource({ id: "r2", kind: "image", title: "logo.svg", mime: "image/svg+xml", sizeBytes: 6_144 });
const pdf = makeResource({ id: "r3", kind: "file", title: "contract-v2.pdf", mime: "application/pdf", sizeBytes: 236_544 });
const txt = makeResource({ id: "r4", kind: "file", title: "notes", mime: "text/plain; charset=utf-8", sizeBytes: 900 });
const json = makeResource({ id: "r5", kind: "file", title: "session-export.json", mime: "application/json", sizeBytes: 48_128 });
const m4a = makeResource({ id: "r6", kind: "audio", title: "standup.m4a", mime: "audio/mp4", sizeBytes: 1_887_437 });
const link = makeResource({ id: "r7", kind: "link", title: "Figma", url: "https://figma.com/file/abc?node=1", mime: "", sizeBytes: 0, contentUrl: "" });

test("sizes are binary: bytes and KB rounded, MB with one decimal", () => {
  assert.equal(sizeWords(0), "0 B");
  assert.equal(sizeWords(1023), "1023 B");
  assert.equal(sizeWords(412_672), "403 KB");
  assert.equal(sizeWords(1_887_437), "1.8 MB");
  assert.equal(sizeWords(32_925_696), "31.4 MB");
  assert.equal(sizeWords(3 * 1024 ** 3), "3.0 GB");
});

test("each kind's title does what the design says; SVG downloads, it has no preview", () => {
  assert.deepEqual([png, svg, pdf, txt, json, m4a, link].map(titleAction), ["lightbox", "download", "tab", "tab", "download", "none", "link"]);
  assert.deepEqual([png, svg, pdf, m4a, link].map(tileKind), ["image", "ext", "ext", "audio", "link"]);
});

test("the title's href: content inline, a forced download, or the link itself", () => {
  assert.equal(titleHref(png), "/api/ticket_resources/r1/content");
  assert.equal(titleHref(pdf), "/api/ticket_resources/r3/content");
  assert.equal(titleHref(svg), "/api/ticket_resources/r2/content?download=1");
  assert.equal(titleHref(link), "https://figma.com/file/abc?node=1");
  assert.equal(titleHref(m4a), "");
  assert.equal(downloadHref(link), "", "a link has no bytes");
});

test("the type words come from the title's extension, else the MIME subtype", () => {
  assert.deepEqual([png, svg, pdf, txt, json, m4a].map(typeWord), ["PNG image", "SVG image", "PDF", "TXT", "JSON", "M4A audio"]);
  assert.equal(extensionLabel("Screenshot", "image/jpeg"), "JPEG");
  assert.equal(extensionLabel("data", "application/vnd.ms-excel"), "EXCEL");
  assert.equal(extensionLabel("blob", "application/octet-stream"), "FILE");
  assert.equal(extensionLabel("blob", ""), "FILE");
});

test("a link's address shows host and path without the scheme", () => {
  assert.equal(addressWords("https://figma.com/file/abc?node=1"), "figma.com/file/abc?node=1");
  assert.equal(addressWords("http://example.com/"), "example.com");
});

test("the row menu per kind; delete is always last", () => {
  assert.deepEqual(menuItems(png), ["view", "download", "rename", "delete"]);
  assert.deepEqual(menuItems(svg), ["download", "rename", "delete"]);
  assert.deepEqual(menuItems(pdf), ["open", "download", "rename", "delete"]);
  assert.deepEqual(menuItems(m4a), ["download", "rename", "delete"]);
  assert.deepEqual(menuItems(link), ["open-link", "copy-address", "rename", "delete"]);
});

test("a link must be http or https with a host, at most 2048 characters", () => {
  assert.equal(linkProblem("https://example.com/a"), "");
  assert.equal(linkProblem("  http://localhost:3000  "), "");
  assert.ok(linkProblem(""));
  assert.ok(linkProblem("example.com"));
  assert.ok(linkProblem("ftp://example.com"));
  assert.ok(linkProblem("javascript:alert(1)"));
  assert.ok(linkProblem(`https://example.com/${"a".repeat(2048)}`).includes("2,048"));
});

test("a title is trimmed, required and at most 200 runes", () => {
  assert.equal(titleProblem(" spec.pdf "), "");
  assert.ok(titleProblem("   "));
  assert.equal(titleProblem("é".repeat(200)), "");
  assert.ok(titleProblem("é".repeat(201)));
});

test("25 MiB is the limit, inclusive", () => {
  assert.ok(!tooLarge(MAX_RESOURCE_BYTES));
  assert.ok(tooLarge(MAX_RESOURCE_BYTES + 1));
});

test("a pasted screenshot is titled by the time it was pasted", () => {
  const at = new Date(2026, 9, 8, 9, 5);
  assert.equal(pastedTitle(at, "image/png"), "Pasted image 09:05.png");
  assert.equal(pastedTitle(at, "image/jpeg"), "Pasted image 09:05.jpg");
  assert.equal(pastedTitle(at, ""), "Pasted image 09:05.png");
});

test("the lightbox steps through the images the browser can show, in list order", () => {
  const second = makeResource({ id: "r8", kind: "image", title: "b.webp", mime: "image/webp" });
  assert.deepEqual(viewableImages([png, svg, pdf, second]).map((r) => r.id), ["r1", "r8"]);
});
