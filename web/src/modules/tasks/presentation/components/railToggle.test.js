import { mount } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { makeTask } from "../../testing/fixtures.js";
import { railToggle } from "./rail.js";

file("tasks/presentation/railToggle");

test("the Tasks button counts what needs the person and opens the drawer instead of following its link", () => {
  const store = { tasks: [makeTask({ pendingReviews: 1 }), makeTask({ id: "t2" })], byTask: { t2: { live: 1, attention: true } }, seeded: true, drawerOpen: false };
  const button = mount(railToggle({ store }));
  assert.deepEqual([button.instance.needsYouCount, button.instance.hasNeedsYou, button.instance.expanded], [2, true, "false"]);
  let prevented = false;
  button.instance.open({ preventDefault: () => (prevented = true) });
  assert.deepEqual([prevented, button.dispatched], [true, [{ name: "rail-open", detail: undefined }]]);
  store.drawerOpen = true;
  assert.deepEqual([button.instance.expanded, button.instance.openAttr], ["true", true]);
  button.instance.close();
  assert.equal(button.dispatched.at(-1).name, "rail-close");
});
