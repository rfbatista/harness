import { assert, file, test } from "../../../shared/testing/test.js";
import { cardsOf, currentOf, itemsOf, notesOf } from "./showcase.js";

file("designsystem/domain/showcase");

const tokens = [{ group: "colors", name: "signal", type: "color", value: "red", note: "" }];
const preview = (css) => ({ label: "L", variant: "v", state: "s", html: "<b></b>", css });
const button = { slug: "button", name: "Button", previews: [preview(".a{color:var(--colors-signal)}"), preview(".b{color:var(--colors-gone);border-color:var(--colors-gone)}")] };
const badge = { slug: "badge", name: "Badge", previews: [preview("")] };

test("each preview is a card; one that names a missing token is flagged with the name once", () => {
  const cards = cardsOf(button, tokens);
  assert.deepEqual(cards.map((c) => [c.key, c.flagged, c.missing]), [["button#0", false, []], ["button#1", true, ["--colors-gone"]]]);
});

test("the list says how many previews and which tokens are missing", () => {
  assert.deepEqual(itemsOf([button, badge], tokens).map((i) => [i.slug, i.countText, i.missing]), [["button", "2 previews", ["--colors-gone"]], ["badge", "1 preview", []]]);
});

test("the open component is the named one, else the first", () => {
  assert.equal(currentOf([button, badge], "badge").slug, "badge");
  assert.equal(currentOf([button, badge], "nope").slug, "button");
  assert.equal(currentOf([], ""), undefined);
});

test("notes are keyed rows", () => {
  assert.deepEqual(notesOf(["a", "a"]), [{ key: "0", text: "a" }, { key: "1", text: "a" }]);
});
