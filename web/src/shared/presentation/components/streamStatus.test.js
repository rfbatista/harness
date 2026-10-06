import { FeedStatus } from "../../domain/feed.js";
import { mount } from "../../testing/alpine.js";
import { assert, file, test } from "../../testing/test.js";
import { feedStatusView } from "../feedStatus.js";
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

test("feedStatusView is the words the stream bar uses, shared with the Design tab", () => {
  assert.deepEqual(feedStatusView("connecting"), { state: "reconnecting", label: "connecting" });
  assert.deepEqual(feedStatusView("live"), { state: "live", label: "live" });
  assert.deepEqual(feedStatusView("paused"), { state: "reconnecting", label: "live updates paused · retrying" });
  assert.deepEqual(feedStatusView("resynced"), { state: "live", label: "live" });
});
