import { assert, file, test } from "../../../shared/testing/test.js";
import {
  changeCounts,
  filterRows,
  formatValue,
  groupCounts,
  newRow,
  parseValue,
  recordOf,
  rowsFromRecord,
  rowStatus,
  sampleOf,
  tokenOf,
} from "./tokens.js";

file("designsystem/domain/tokens");

const colour = { group: "colors", name: "signal", type: "color", value: "oklch(0.5 0.095 182)", note: "Primary action" };
const headline = {
  group: "typography",
  name: "headline",
  type: "typography",
  value: { fontFamily: "Inter, sans-serif", fontSize: "1.5rem", fontWeight: "600" },
  note: "",
};
const record = { tokens: [colour, headline], principles: "# Calm\n", components: [{ slug: "button" }] };

test("a composite value is typed as key: value pairs and read back as an object", () => {
  assert.equal(formatValue("typography", headline.value), "fontFamily: Inter, sans-serif; fontSize: 1.5rem; fontWeight: 600");
  assert.deepEqual(parseValue("typography", "fontFamily: Inter, sans-serif; fontSize: 1.5rem; fontWeight: 600"), headline.value);
  assert.equal(formatValue("color", "#fff"), "#fff");
  assert.equal(parseValue("color", " #fff "), "#fff");
});

test("a composite part without a colon is kept under an empty key, so the server names it", () => {
  assert.deepEqual(parseValue("shadow", "offsetX: 0; blur"), { offsetX: "0", "": "blur" });
});

test("rows are the record's tokens in order, and the record comes back from them with principles and components untouched", () => {
  const rows = rowsFromRecord(record);
  assert.equal(rows.length, 2);
  assert.equal(rows[0].text, "oklch(0.5 0.095 182)");
  assert.deepEqual(recordOf(rows, record.principles, record.components), record);
});

test("a row's group and name are trimmed when the record is built; the value is not", () => {
  const row = { ...newRow("n1", "colors", "color"), group: " colors ", name: " ink ", text: "#000" };
  assert.deepEqual(tokenOf(row), { group: "colors", name: "ink", type: "color", value: "#000", note: "" });
});

test("a row is added, edited or neither against the saved tokens", () => {
  const rows = rowsFromRecord(record);
  assert.equal(rowStatus(rows[0], record.tokens), "");
  rows[0].text = "#fff";
  assert.equal(rowStatus(rows[0], record.tokens), "edited");
  assert.equal(rowStatus(newRow("n1", "colors", "color"), record.tokens), "added");
});

test("changes are counted as edited, added and removed", () => {
  const rows = rowsFromRecord(record);
  assert.deepEqual(changeCounts(rows, record.tokens), { edited: 0, added: 0, removed: 0 });
  rows[0].note = "changed";
  rows.push(newRow("n1", "colors", "color"));
  rows.splice(1, 1);
  assert.deepEqual(changeCounts(rows, record.tokens), { edited: 1, added: 1, removed: 1 });
});

test("the filter matches group, name, type, value and note, and one group", () => {
  const rows = rowsFromRecord(record);
  assert.equal(filterRows(rows, "", "").length, 2);
  assert.deepEqual(filterRows(rows, "SIGNAL", "").map((r) => r.name), ["signal"]);
  assert.deepEqual(filterRows(rows, "inter", "").map((r) => r.name), ["headline"]);
  assert.deepEqual(filterRows(rows, "primary", "").map((r) => r.name), ["signal"]);
  assert.deepEqual(filterRows(rows, "", "typography").map((r) => r.name), ["headline"]);
  assert.equal(filterRows(rows, "signal", "typography").length, 0);
});

test("groups are counted in the order they first appear", () => {
  const rows = rowsFromRecord({ tokens: [colour, { ...colour, name: "ink" }, headline] });
  assert.deepEqual(groupCounts(rows), [
    { group: "colors", count: 2 },
    { group: "typography", count: 1 },
  ]);
});

test("a colour sample paints the value as typed", () => {
  const row = { ...newRow("n1", "colors", "color"), text: "oklch(0.52 0.12 65)" };
  const s = sampleOf(row);
  assert.equal(s.kind, "swatch");
  assert.ok(s.style.includes("background:oklch(0.52 0.12 65)"), s.style);
  assert.equal(sampleOf({ ...row, text: "#ff0000" }).style.includes("background:#ff0000"), true);
});

test("a colour the browser cannot read gets the hatched fallback instead of the value", () => {
  const row = { ...newRow("n1", "colors", "color"), text: "oklch(0.52 0.18" };
  const s = sampleOf(row, () => false);
  assert.equal(s.kind, "swatch");
  assert.equal(s.invalid, true);
  assert.ok(!s.style.includes("oklch(0.52 0.18"), s.style);
});

test("typography and font tokens sample Aa in the typed face", () => {
  const typed = sampleOf({ ...newRow("n1", "typography", "typography"), text: formatValue("typography", headline.value) });
  assert.equal(typed.kind, "type");
  assert.equal(typed.glyph, "Aa");
  assert.ok(typed.style.includes("font-size:1.5rem") && typed.style.includes("font-family:Inter, sans-serif") && typed.style.includes("font-weight:600"), typed.style);
  const font = sampleOf({ ...newRow("n2", "fonts", "font"), text: "Georgia, serif" });
  assert.ok(font.style.includes("font-family:Georgia, serif"), font.style);
});

test("a dimension is a square, a radius a rounded box, a size in a type group Aa", () => {
  assert.equal(sampleOf({ ...newRow("n1", "space", "dimension"), text: "12px" }).kind, "space");
  assert.ok(sampleOf({ ...newRow("n1", "space", "dimension"), text: "12px" }).style.includes("min(12px"));
  const radius = sampleOf({ ...newRow("n2", "radius", "dimension"), text: "8px" });
  assert.equal(radius.kind, "radius");
  assert.ok(radius.style.includes("border-radius:8px"), radius.style);
  const size = sampleOf({ ...newRow("n3", "type", "dimension"), text: "0.875rem" });
  assert.equal(size.kind, "type");
  assert.ok(size.style.includes("font-size:0.875rem"), size.style);
});

test("a shadow sample builds box-shadow from the typed parts", () => {
  const s = sampleOf({ ...newRow("n1", "shadows", "shadow"), text: "offsetX: 0; offsetY: 4px; blur: 8px; spread: 0; color: #0003" });
  assert.equal(s.kind, "shadow");
  assert.ok(s.style.includes("box-shadow:0 4px 8px 0 #0003"), s.style);
});

test("a duration sample is a bar as long as the time, capped", () => {
  const short = sampleOf({ ...newRow("n1", "motion", "duration"), text: "120ms" });
  const long = sampleOf({ ...newRow("n2", "motion", "duration"), text: "2s" });
  assert.equal(short.kind, "motion");
  assert.ok(short.style.includes("12% 100%"), short.style);
  assert.ok(long.style.includes("100% 100%"), long.style);
  assert.equal(sampleOf({ ...newRow("n3", "motion", "duration"), text: "soon" }).invalid, true);
});

test("a number has no picture, only its value", () => {
  assert.equal(sampleOf({ ...newRow("n1", "scale", "number"), text: "1.25" }).kind, "number");
});
