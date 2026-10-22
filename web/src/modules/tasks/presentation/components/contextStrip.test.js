import { mount } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { contextStrip } from "./contextStrip.js";

file("tasks/presentation/contextStrip");

test("opening a band closes the one that was open, and pressing the open band closes it", () => {
  const { instance } = mount(contextStrip());
  assert.equal(instance.expanded("feature"), false);
  instance.toggle("feature");
  assert.deepEqual([instance.expanded("feature"), instance.expanded("reviews")], [true, false]);
  instance.toggle("reviews");
  assert.deepEqual([instance.expanded("feature"), instance.expanded("reviews")], [false, true]);
  instance.toggle("reviews");
  assert.equal(instance.expanded("reviews"), false);
});
