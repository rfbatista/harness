import { FeedStatus } from "../../domain/feed.js";
import { mount } from "../../testing/alpine.js";
import { assert, file, test } from "../../testing/test.js";
import { streamStatus } from "./streamStatus.js";

file("shared/presentation/streamStatus");

test("maps feed status to the design system's status states", () => {
  const { instance } = mount(streamStatus());
  assert.equal(instance.state, "reconnecting");
  instance.update({ detail: FeedStatus.LIVE });
  assert.deepEqual([instance.state, instance.label], ["live", "live"]);
  instance.update({ detail: FeedStatus.PAUSED });
  assert.equal(instance.state, "reconnecting");
  instance.update({ detail: FeedStatus.RESYNCED });
  assert.equal(instance.state, "live", "resynced reads as live");
});
