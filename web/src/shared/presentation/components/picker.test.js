import { assert, file, test } from "../../testing/test.js";
import { filterChoices } from "./picker.js";

file("shared/presentation/picker");

test("a picker's filter matches every typed word, in any case and order", () => {
  const choices = [{ id: "1", label: "Checkout redesign" }, { id: "2", label: "Landing page" }, { id: "3", label: "Checkout API" }];
  assert.deepEqual(filterChoices(choices, "").map((c) => c.id), ["1", "2", "3"]);
  assert.deepEqual(filterChoices(choices, "  CHECK ").map((c) => c.id), ["1", "3"]);
  assert.deepEqual(filterChoices(choices, "design check").map((c) => c.id), ["1"]);
  assert.deepEqual(filterChoices(choices, "nothing"), []);
});
