// The browser's copy of the token custom-property rule and of the preview
// document and validation (web/src/modules/designsystem/domain/preview.js)
// answers what the Go rule answers (internal/domain/designpreview.go) on the
// fixed cases both read.
// Run: node --test design-system/preview-parity.test.mjs
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { previewDocument, rootRule, validateComponents } from "../web/src/modules/designsystem/domain/preview.js";

const cases = JSON.parse(readFileSync(fileURLToPath(new URL("../internal/domain/testdata/design_preview_cases.json", import.meta.url)), "utf8"));

for (const c of cases) {
  test(c.name, () => {
    assert.equal(rootRule(c.record.tokens), c.root);
    if (c.document) assert.equal(previewDocument(c.record.tokens, c.record.components[0].previews[0]), c.document);
    const got = validateComponents(c.record, c.previous ?? { tokens: [], components: [] }).map((e) => `${e.code} ${e.path}`);
    assert.deepEqual(got, c.errors);
  });
}
