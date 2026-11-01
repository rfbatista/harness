import { assert, file, test } from "../../../shared/testing/test.js";
import { previewDocument, rootRule, tokenProperty, validateComponents } from "./preview.js";

file("designsystem/domain/preview");

const colour = { group: "colors", name: "signal", type: "color", value: "red", note: "" };
const preview = { label: "L", variant: "v", state: "s", html: "<b>x</b>", css: ".a{color:var(--colors-signal)}" };

test("a token is --group-name and a composite member adds its key in kebab case", () => {
  assert.equal(tokenProperty("colors", "signal", ""), "--colors-signal");
  assert.equal(tokenProperty("typography", "headline", "fontFamily"), "--typography-headline-font-family");
});

test("a document built from unsaved tokens differs from the saved one only in the :root block", () => {
  const saved = previewDocument([colour], preview);
  const edited = previewDocument([{ ...colour, value: "blue" }], preview);
  assert.equal(edited, saved.replace(rootRule([colour]), rootRule([{ ...colour, value: "blue" }])));
  assert.ok(edited !== saved);
});

test("a preview naming a token the unsaved tokens lack is UNKNOWN_TOKEN", () => {
  const record = { tokens: [], components: [{ slug: "a", previews: [preview] }] };
  assert.deepEqual(validateComponents(record, { tokens: [colour], components: [] }).map((e) => e.code), ["TOKEN_IN_USE"]);
  assert.deepEqual(validateComponents(record).map((e) => e.code), ["UNKNOWN_TOKEN"]);
});
