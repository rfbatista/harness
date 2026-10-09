import { mount } from "../../testing/alpine.js";
import { assert, file, test } from "../../testing/test.js";
import { streamAnnouncer } from "./streamAnnouncer.js";

file("shared/presentation/streamAnnouncer");

test("it says each announcement, the same one again too", () => {
  const bar = mount(streamAnnouncer());
  bar.instance.say({ detail: "“Fix flaky test” is waiting on you." });
  assert.equal(bar.instance.text, "");
  bar.tick();
  assert.equal(bar.instance.text, "“Fix flaky test” is waiting on you.");
  bar.instance.say({ detail: "“Fix flaky test” is waiting on you." });
  assert.equal(bar.instance.text, "", "cleared first, so a reader hears it again");
});
