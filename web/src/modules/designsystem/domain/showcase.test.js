import { assert, file, test } from "../../../shared/testing/test.js";
import { cardsOf, cleanComponents, componentsChanged, copyComponents, currentIndex, itemsOf, newComponent, newPreview, notesOf, problemLabel } from "./showcase.js";

file("designsystem/domain/showcase");

const tokens = [{ group: "colors", name: "signal", type: "color", value: "red", note: "" }];
const preview = (css) => ({ label: "L", variant: "v", state: "s", html: "<b></b>", css });
const button = { slug: "button", name: "Button", purpose: "p", do: [" a ", ""], dont: [], previews: [preview(".a{color:var(--colors-signal)}"), preview(".b{color:var(--colors-gone);border-color:var(--colors-gone)}")] };
const badge = { slug: "badge", name: "Badge", purpose: "", do: [], dont: [], previews: [preview("")] };

test("each preview is a card; one that names a missing token is flagged with the name once", () => {
  const cards = cardsOf(button, 0, tokens);
  assert.deepEqual(cards.map((c) => [c.key, c.flagged, c.missing]), [["0#0", false, []], ["0#1", true, ["--colors-gone"]]]);
});

test("the list says how many previews, which tokens are missing and how many problems", () => {
  const items = itemsOf([button, badge], tokens, [{ path: "/components/1/slug" }]);
  assert.deepEqual(items.map((i) => [i.slug, i.countText, i.missing, i.problems, i.flagged]), [["button", "2 previews", ["--colors-gone"], 0, true], ["badge", "1 preview", [], 1, true]]);
});

test("the open component is the position asked for, else the first", () => {
  assert.equal(currentIndex([button, badge], 1), 1);
  assert.equal(currentIndex([button, badge], 5), 0);
  assert.equal(currentIndex([], 0), -1);
});

test("notes are keyed rows", () => {
  assert.deepEqual(notesOf(["a", "a"]), [{ key: "0", text: "a" }, { key: "1", text: "a" }]);
});

test("a new component has a free valid slug and one preview", () => {
  assert.equal(newComponent([]).slug, "new-component");
  assert.equal(newComponent([{ slug: "new-component" }]).slug, "new-component-2");
  assert.equal(newComponent([]).previews.length, 1);
  assert.equal(newPreview(2).label, "Variant 3");
});

test("cleaning trims and drops blank notes, and an unedited draft is not a change", () => {
  assert.deepEqual(cleanComponents([button])[0].do, ["a"]);
  assert.equal(componentsChanged(copyComponents([badge]), [badge]), false);
  const edited = copyComponents([badge]);
  edited[0].purpose = "x";
  assert.equal(componentsChanged(edited, [badge]), true);
  const notes = copyComponents([badge]);
  notes[0].do.push("  ");
  assert.equal(componentsChanged(notes, [badge]), false, "a blank note line is not a change");
});

test("a problem's path reads as where it is", () => {
  assert.equal(problemLabel("/components/0/previews/1/css", [button]), "Components › button › L");
  assert.equal(problemLabel("/components/1/slug", [button, { ...badge, slug: " " }]), "Components › …");
  assert.equal(problemLabel("/tokens", []), "Tokens");
  assert.equal(problemLabel("/principles", []), "Design system");
});
