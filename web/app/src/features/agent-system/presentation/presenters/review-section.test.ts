// The Review section and the Score section's line (ticket 04), through the presenter on the in-memory store.

import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { apiClient } from "../../../../shared/infrastructure/api-client";
import { httpSettingsSource } from "../../infrastructure/http-settings-source";
import { fakeStore } from "../../testing/fake-store";
import { memoryScopeAddress } from "../../testing/memory-scope-address";
import { AgentSystemPresenter } from "./agent-system-presenter";

const MODELS = [
  { value: "default", label: "default: the CLI chooses" },
  { value: "fable", label: "fable" },
  { value: "opus", label: "opus" },
  { value: "sonnet", label: "sonnet" },
  { value: "haiku", label: "haiku" },
];

function setup(opts: { repository?: string; models?: typeof MODELS } = {}) {
  const store = fakeStore();
  store.repositories.push({ id: "r1", name: "harness" }, { id: "r2", name: "blog" });
  store.runCommands.set("r1", [{ name: "web", command: "make run" }, { name: "docs", command: "make docs" }]);
  const presenter = new AgentSystemPresenter({
    source: httpSettingsSource(apiClient(store.fetch)),
    address: memoryScopeAddress(opts.repository ?? ""),
    projectId: "p1",
    projectName: "pessoal",
    architectureHref: (id) => `/a/${id}`,
    scoreHref: "/projects/p1/score",
    models: opts.models ?? MODELS,
  });
  const layer = (key: string) => presenter.viewModel.layers.find((l) => l.key === key)!;
  const review = () => layer("review");
  const row = (path: string) => review().rows.find((r) => r.path === path)!;
  const patches = () => store.requests.filter((r) => r.startsWith("PATCH"));
  return { store, presenter, vm: () => presenter.viewModel, layer, review, row, patches };
}

describe("the Review section", () => {
  test("it sits after Guard and before Score, with the critic and its standards", async () => {
    const { presenter, vm, review } = setup();
    await presenter.start();
    assert.deepEqual(vm().layers.map((l) => l.key), ["learn", "guard", "review", "score"]);
    assert.deepEqual(review().rows.map((r) => r.path), ["review.critic.enabled", "review.critic.model", "review.critic.standards"]);
  });

  test("the critic switch reads the harness default, and a save sends only that path", async () => {
    const { presenter, row, store, patches } = setup();
    await presenter.start();
    assert.deepEqual([row("review.critic.enabled").value, row("review.critic.enabled").source.text], [true, "harness default"]);
    presenter.edit("review.critic.enabled", false);
    await presenter.save();
    assert.equal(patches().length, 1);
    assert.deepEqual(store.project, { "review.critic.enabled": false });
    assert.deepEqual([row("review.critic.enabled").value, row("review.critic.enabled").source.text], [false, "project"]);
  });

  test("the model select offers the review-mode model first, then the models the harness can call", async () => {
    const { presenter, row } = setup();
    await presenter.start();
    const model = row("review.critic.model");
    assert.equal(model.kind, "select");
    assert.deepEqual(model.options.map((o) => o.value), ["", "default", "fable", "opus", "sonnet", "haiku"]);
    assert.equal(model.options[0]?.label, "Review-mode model (opus)");
    assert.deepEqual([model.selected, model.source.text, model.action], ["", "harness default", "none"]);
  });

  test("choosing a model drafts it, Save sends it; choosing the review-mode model again resets the field", async () => {
    const { presenter, row, store } = setup();
    await presenter.start();
    presenter.selectModel("sonnet");
    assert.deepEqual([row("review.critic.model").selected, row("review.critic.model").draft], ["sonnet", true]);
    await presenter.save();
    assert.equal(store.project["review.critic.model"], "sonnet");
    assert.deepEqual([row("review.critic.model").selected, row("review.critic.model").source.text, row("review.critic.model").action], ["sonnet", "project", "reset"]);
    presenter.selectModel("");
    await presenter.save();
    assert.equal("review.critic.model" in store.project, false);
    assert.equal(row("review.critic.model").selected, "");
  });

  test("a model the harness does not offer is refused on the row before a save, naming the model", async () => {
    const { presenter, row, vm } = setup();
    await presenter.start();
    presenter.edit("review.critic.model", "gpt-9");
    assert.equal(row("review.critic.model").error?.code, "MODEL_UNAVAILABLE");
    assert.match(row("review.critic.model").error?.message ?? "", /gpt-9/);
    assert.equal(vm().canSave, false);
  });

  test("when the page does not know the models, the store's MODEL_UNAVAILABLE lands on the row and keeps the value", async () => {
    const { presenter, row, vm } = setup({ models: [] });
    await presenter.start();
    presenter.edit("review.critic.model", "gpt-9");
    assert.equal(vm().canSave, true);
    await presenter.save();
    assert.equal(row("review.critic.model").error?.code, "MODEL_UNAVAILABLE");
    assert.equal(row("review.critic.model").selected, "gpt-9");
  });

  test("standards are chips: add a path, remove one, Save sends the whole list", async () => {
    const { presenter, row, store } = setup();
    await presenter.start();
    assert.deepEqual(row("review.critic.standards").items, ["AGENTS.md", "CLAUDE.md", "context map"]);
    assert.equal(row("review.critic.standards").placeholder, "add a path");
    presenter.edit("review.critic.standards", ["AGENTS.md", "CLAUDE.md", "context map", "DESIGN.md"]);
    await presenter.save();
    assert.deepEqual(store.project["review.critic.standards"], ["AGENTS.md", "CLAUDE.md", "context map", "DESIGN.md"]);
  });

  test("a standards entry with an unclosed bracket is refused on the row by the form", async () => {
    const { presenter, row } = setup();
    await presenter.start();
    presenter.edit("review.critic.standards", ["AGENTS.md", "docs/[x"]);
    assert.equal(row("review.critic.standards").error?.code, "INVALID_GLOB");
  });

  test("the store's error for entry 1 of protected paths reaches that row with the entry named", async () => {
    const { presenter, store, layer } = setup();
    await presenter.start();
    presenter.edit("guard.protected_paths", ["check.sh", "ok"]);
    store.refuseNextSave([{ field: "guard.protected_paths[1]", code: "INVALID_GLOB", message: "not a valid glob: ok" }]);
    await presenter.save();
    const paths = layer("guard").rows.find((r) => r.path === "guard.protected_paths")!;
    assert.equal(paths.error?.code, "INVALID_GLOB");
    assert.match(paths.error?.message ?? "", /entry 2/);
  });
});

describe("Visual QA", () => {
  test("at project scope the block asks for a repository", async () => {
    const { presenter, review } = setup();
    await presenter.start();
    assert.deepEqual([review().visualQa?.declared, review().visualQa?.canDeclare], [false, false]);
  });

  test("in a repository without one it can be declared; declaring drafts a whole block with the repository's first run command", async () => {
    const { presenter, review, vm } = setup({ repository: "r1" });
    await presenter.start();
    assert.deepEqual([review().visualQa?.declared, review().visualQa?.canDeclare], [false, true]);
    presenter.declareVisualQa();
    assert.equal(review().visualQa?.declared, true);
    assert.equal(review().visualQa?.ui.start_command, "make run");
    assert.deepEqual(vm().runCommands.map((c) => c.name), ["web", "docs"]);
    assert.equal(vm().unsaved, 1);
  });

  test("Save sends the whole declaration at review.visual_qa.ui", async () => {
    const { presenter, store } = setup({ repository: "r1" });
    await presenter.start();
    presenter.declareVisualQa();
    presenter.editVisualQa({ url: "http://127.0.0.1:8080", routes: ["/", "/notifications"], viewports: ["1440x900"], ui_paths: ["web/**"] });
    await presenter.save();
    assert.deepEqual(store.repositoryValues("r1")["review.visual_qa.ui"], { start_command: "make run", url: "http://127.0.0.1:8080", routes: ["/", "/notifications"], viewports: ["1440x900"], ui_paths: ["web/**"] });
  });

  test("removing the declaration resets the field as a whole", async () => {
    const { presenter, store, review } = setup({ repository: "r1" });
    store.repositoryValues("r1")["review.visual_qa.ui"] = { start_command: "make run", url: "http://127.0.0.1:8080", routes: ["/"], viewports: ["390x844"], ui_paths: [] };
    await presenter.start();
    assert.deepEqual([review().visualQa?.declared, review().visualQa?.source.text, review().visualQa?.action], [true, "override", "reset"]);
    presenter.removeVisualQa();
    await presenter.save();
    assert.equal("review.visual_qa.ui" in store.repositoryValues("r1"), false);
    assert.equal(review().visualQa?.declared, false);
  });

  test("a route without a leading slash is refused on the block before a save, naming its line", async () => {
    const { presenter, review, vm } = setup({ repository: "r1" });
    await presenter.start();
    presenter.declareVisualQa();
    presenter.editVisualQa({ routes: ["/", "/projects", "projects/settings"] });
    const error = review().visualQa?.error;
    assert.equal(error?.code, "INVALID_ROUTE");
    assert.match(error?.message ?? "", /line 3/);
    assert.equal(vm().canSave, false);
  });

  test("the store's INVALID_ROUTE for a route lands on the block with its line, keeping the values", async () => {
    const { presenter, review, store } = setup({ repository: "r1" });
    await presenter.start();
    presenter.declareVisualQa();
    presenter.editVisualQa({ routes: ["/", "/ok"] });
    store.refuseNextSave([{ field: "review.visual_qa.ui.routes[1]", code: "INVALID_ROUTE", message: "a route is a path starting with /" }]);
    await presenter.save();
    assert.equal(review().visualQa?.error?.code, "INVALID_ROUTE");
    assert.match(review().visualQa?.error?.message ?? "", /line 2/);
    assert.deepEqual(review().visualQa?.ui.routes, ["/", "/ok"]);
  });
});

describe("the Score section", () => {
  test("it links to the Score page with the last finished run's score and cost", async () => {
    const { presenter, layer, store } = setup();
    store.scorecards = [{ status: "cancelled", aggregate: { mean: 0.1 }, cost_usd: 0.2, cases: [] }, { status: "finished", aggregate: { mean: 0.82 }, cost_usd: 1.14, cases: [{}, {}, {}] }];
    await presenter.start();
    assert.deepEqual([layer("score").link?.href, layer("score").link?.text], ["/projects/p1/score", "Score page · 3 cases · last run 0.82 · $1.14"]);
  });

  test("with no finished run the line has a dash", async () => {
    const { presenter, layer } = setup();
    await presenter.start();
    assert.equal(layer("score").link?.text, "Score page · last run —");
  });

  test("the trajectory recording default is its row", async () => {
    const { presenter, layer } = setup();
    await presenter.start();
    assert.deepEqual(layer("score").rows.map((r) => r.path), ["score.trajectory_recording.default"]);
  });
});
