import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { apiClient } from "../../../shared/infrastructure/api.js";
import { fakeFetch, jsonResponse } from "../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { instructionFilesGateway } from "./instruction-files-gateway.js";

file("architecture/infrastructure/instruction-files-gateway");

const meta = { path: "CLAUDE.md", kind: "memory", version: 3, size: 20, updated_at: "2026-10-31T10:00:00Z" };

/** A gateway whose server answers `answer(route, body)`; records each call. */
function rig(answer, followed = []) {
  const { fetch, calls } = fakeFetch((url, init) => answer(url.replace("/api/", ""), JSON.parse(init.body ?? "null")));
  const feed = { follow: (path, handlers) => (followed.push({ path, handlers }), () => followed.push("stopped")) };
  const gateway = instructionFilesGateway(apiClient({ base: "/api", fetch }), feed);
  const sent = () => calls.map((c) => [c.url.replace("/api/", ""), JSON.parse(c.init.body)]);
  return { gateway, sent, followed };
}

test("list sends the repository and reads the files and the repository's copies", async () => {
  const { gateway, sent } = rig(() =>
    jsonResponse(200, { files: [meta], repository_copies: [{ path: "AGENTS.md", kind: "memory", differs: true }] }),
  );
  const list = await gateway.list("r1");
  assert.deepEqual(sent(), [["list_instruction_files", { repository_id: "r1" }]]);
  assert.deepEqual(list.files, [{ path: "CLAUDE.md", kind: "memory", version: 3, size: 20, updatedAt: "2026-10-31T10:00:00Z" }]);
  assert.deepEqual(list.repositoryCopies, [{ path: "AGENTS.md", kind: "memory", differs: true }]);
});

test("list tolerates a missing copy list", async () => {
  const { gateway } = rig(() => jsonResponse(200, { files: [] }));
  assert.deepEqual(await gateway.list("r1"), { files: [], repositoryCopies: [] });
});

test("get sends the path and returns the content with the meta", async () => {
  const { gateway, sent } = rig(() => jsonResponse(200, { ...meta, content: "# Rules" }));
  const file = await gateway.get("r1", "CLAUDE.md");
  assert.deepEqual(sent(), [["get_instruction_file", { repository_id: "r1", path: "CLAUDE.md" }]]);
  assert.equal(file.content, "# Rules");
  assert.equal(file.version, 3);
});

test("a body that is not a file is a BAD_RESPONSE", async () => {
  const { gateway } = rig(() => jsonResponse(200, { nope: true }));
  await assert.rejects(gateway.get("r1", "CLAUDE.md"), Codes.BAD_RESPONSE);
});

test("save sends the contract's body; base version 0 creates", async () => {
  const { gateway, sent } = rig(() => jsonResponse(200, { ...meta, version: 1 }));
  const saved = await gateway.save("r1", { path: "CLAUDE.md", kind: "memory", baseVersion: 0, content: "x" });
  assert.deepEqual(sent(), [["save_instruction_file", { repository_id: "r1", path: "CLAUDE.md", kind: "memory", base_version: 0, content: "x" }]]);
  assert.equal(saved.version, 1);
});

test("rename, delete, import and remove send the contract's bodies", async () => {
  const { gateway, sent } = rig((route) => {
    if (route === "delete_instruction_file") return new Response(null, { status: 204 });
    if (route === "import_instruction_files") return jsonResponse(200, { files: [meta] });
    if (route === "remove_instruction_files_from_repository") return jsonResponse(200, { commit: "abc1234" });
    return jsonResponse(200, { ...meta, path: "AGENTS.md" });
  });
  const renamed = await gateway.rename("r1", { path: "CLAUDE.md", newPath: "AGENTS.md", baseVersion: 3 });
  assert.equal(renamed.path, "AGENTS.md");
  assert.equal(await gateway.remove("r1", { path: "AGENTS.md", baseVersion: 3 }), undefined);
  const imported = await gateway.importFiles("r1", ["CLAUDE.md"]);
  assert.equal(imported.length, 1);
  assert.equal(imported[0].path, "CLAUDE.md");
  assert.deepEqual(await gateway.removeFromRepository("r1", ["CLAUDE.md"]), { commit: "abc1234" });
  assert.deepEqual(sent(), [
    ["rename_instruction_file", { repository_id: "r1", path: "CLAUDE.md", new_path: "AGENTS.md", base_version: 3 }],
    ["delete_instruction_file", { repository_id: "r1", path: "AGENTS.md", base_version: 3 }],
    ["import_instruction_files", { repository_id: "r1", paths: ["CLAUDE.md"] }],
    ["remove_instruction_files_from_repository", { repository_id: "r1", paths: ["CLAUDE.md"] }],
  ]);
});

test("every error code of the contract reaches the caller with its message", async () => {
  const codes = {
    INSTRUCTION_NOT_FOUND: 404,
    INSTRUCTION_CHANGED: 409,
    INSTRUCTION_EXISTS: 409,
    INSTRUCTION_NOT_IMPORTED: 409,
    CHECKOUT_DIRTY: 409,
    WRONG_BRANCH: 409,
    REPOSITORY_NOT_FOUND: 404,
  };
  for (const [code, status] of Object.entries(codes)) {
    const { gateway } = rig(() => jsonResponse(status, { error: `refused ${code}`, code }));
    const err = await assert.rejects(gateway.get("r1", "x"), code);
    assert.equal(err.message, `refused ${code}`);
    assert.equal(err.status, status);
  }
});

test("INSTRUCTION_INVALID carries its field errors", async () => {
  const errors = [{ path: "/path", code: "ABSOLUTE_PATH", message: "Use a path inside the repository." }];
  const { gateway } = rig(() => jsonResponse(400, { error: "invalid", code: "INSTRUCTION_INVALID", details: { errors } }));
  const err = await assert.rejects(gateway.save("r1", { path: "/x", kind: "memory", baseVersion: 0, content: "" }), "INSTRUCTION_INVALID");
  assert.ok(err instanceof StructuredError);
  assert.deepEqual(err.details.errors, errors);
  assert.deepEqual(err.fieldErrors, errors);
});

test("the feed event is read for this repository only", () => {
  const followed = [];
  const { gateway } = rig(() => jsonResponse(200, {}), followed);
  const seen = [];
  const stop = gateway.follow("p1", "r1", { onChanged: (c) => seen.push(c) });
  const { path, handlers } = followed[0];
  assert.equal(path, "/events?project_id=p1");
  handlers.onMessage({ instruction_file_changed: { repository_id: "r1", path: "CLAUDE.md", version: 4 } });
  handlers.onMessage({ instruction_file_changed: { repository_id: "r2", path: "CLAUDE.md", version: 5 } });
  handlers.onMessage({ repository_architecture_changed: { repository_id: "r1", version: 6 } });
  handlers.onMessage({ instruction_file_changed: { repository_id: "r1", path: "old.md", version: 0 } });
  assert.deepEqual(seen, [{ path: "CLAUDE.md", version: 4 }, { path: "old.md", version: 0 }]);
  stop();
  assert.equal(followed[1], "stopped");
});
