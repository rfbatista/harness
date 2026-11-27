import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { DtoError } from "../../../../shared/infrastructure/dto/dto-error";
import { apiClient } from "../../../../shared/infrastructure/api-client";
import { SettingsError } from "../../domain/settings";
import { httpSettingsSource } from "../http-settings-source";
import { SettingsPropsDto } from "./settings-props-dto";
import { SettingsViewDto } from "./settings-view-dto";

const field = (value: unknown, source = "harness") => ({ value, source });
const guard = { gates: [{ kind: "build", command: field("go build ./..."), timeout_seconds: field(600) }], protected_paths: field(["check.sh"]), lockfile_rule: { enabled: field(true) }, retry_limit: field(3), tests_fail_first: { available: field(false) } };
const view = (over: Record<string, unknown> = {}) => ({
  effective: { learn: { context_map: { enabled: field(true) }, task_memory: { require_update: field(true) } }, guard, score: { trajectory_recording: { default: field(false) } }, ...over },
  values: {},
});

describe("SettingsViewDto", () => {
  test("reads the fields the page shows and ignores the rest", () => {
    const dto = SettingsViewDto.fromJSON({ ...view({ missing_required_gates: ["lockfile"] }), extra: 1 });
    assert.equal(dto.context_map_enabled.value, true);
    assert.deepEqual(dto.missing_required_gates, ["lockfile"]);
  });

  test("updated_at is the last save of the level: a time, or null when never saved or not answered", () => {
    assert.equal(SettingsViewDto.fromJSON({ ...view(), updated_at: "2026-11-26T20:00:00Z" }).updated_at, Date.parse("2026-11-26T20:00:00Z"));
    assert.equal(SettingsViewDto.fromJSON({ ...view(), updated_at: null }).updated_at, null);
    assert.equal(SettingsViewDto.fromJSON(view()).updated_at, null);
    assert.throws(() => SettingsViewDto.fromJSON({ ...view(), updated_at: "yesterday" }), DtoError);
  });

  test("an answer without missing_required_gates has none", () => {
    assert.deepEqual(SettingsViewDto.fromJSON(view()).missing_required_gates, []);
  });

  test("a field of the wrong type or an unknown source names its path", () => {
    assert.throws(() => SettingsViewDto.fromJSON(view({ learn: { context_map: { enabled: field("yes") }, task_memory: { require_update: field(true) } } })), (e: unknown) => e instanceof DtoError && e.path === "effective.learn.context_map.enabled.value");
    assert.throws(() => SettingsViewDto.fromJSON(view({ score: { trajectory_recording: { default: field(false, "everywhere") } } })), DtoError);
  });
});

test("an answer that is not the contract's is BAD_RESPONSE, not a crash", async () => {
  const source = httpSettingsSource(apiClient(async () => new Response(JSON.stringify({ nope: true }), { status: 200 })));
  await assert.rejects(source.view("p1", { kind: "project" }), (e: unknown) => e instanceof SettingsError && e.code === "BAD_RESPONSE");
});

test("the island's props carry the project, and the page reads the repository from its address", () => {
  const props = SettingsPropsDto.fromJSON({ project_id: "p1", project_name: "pessoal" });
  assert.deepEqual(props.toJSON(), { project_id: "p1", project_name: "pessoal", repository: "", models: [] });
  assert.throws(() => SettingsPropsDto.fromJSON({ project_id: "p1" }), DtoError);
});
