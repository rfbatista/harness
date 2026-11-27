import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { memoryFeed } from "../../../shared/infrastructure/memory-feed";
import { httpSettingsFeed, toSettingsChange } from "./http-settings-feed";

describe("the project feed's settings messages", () => {
  test("an agent_system_settings message is a change of the project or of one repository; any other kind, or another project, is none", () => {
    assert.deepEqual(toSettingsChange({ agent_system_settings: { project_id: "p1" } }, "p1"), { repositoryId: "" });
    assert.deepEqual(toSettingsChange({ agent_system_settings: { project_id: "p1", repository_id: "r1" } }, "p1"), { repositoryId: "r1" });
    assert.equal(toSettingsChange({ agent_system_settings: { project_id: "p2" } }, "p1"), null);
    assert.equal(toSettingsChange({ session: {} }, "p1"), null);
    assert.equal(toSettingsChange({ agent_system_settings: {} }, "p1"), null);
  });

  test("it follows the project's feed and hands over only settings changes, until stopped", () => {
    const feed = memoryFeed();
    const seen: string[] = [];
    const stop = httpSettingsFeed(feed).follow("p1", { onChange: (c) => seen.push(c.repositoryId || "project") });
    feed.emit("/events?project_id=p1", { ticket: {} });
    feed.emit("/events?project_id=p1", { agent_system_settings: { project_id: "p1", repository_id: "r2" } });
    stop();
    feed.emit("/events?project_id=p1", { agent_system_settings: { project_id: "p1" } });
    assert.deepEqual(seen, ["r2"]);
  });
});
