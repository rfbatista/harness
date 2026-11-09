import { assert, file, test } from "../../../shared/testing/test.js";
import {
  addRow, canSave, draftFromService, fieldOf, formTitle, newDraft, removeRow, setKind, toInput,
} from "./serviceForm.js";

file("services/domain/serviceForm");

const service = (o = {}) => ({
  id: "s1", projectId: "p1", name: "api", kind: "command", repositoryId: "r1", exportRepositoryEnv: true, env: "PORT=8080",
  command: { line: "make air", subdir: "cmd/api", ports: [8080, 9090] }, image: null, status: "stopped", run: null, ...o,
});
const image = (o = {}) => service({
  name: "db", kind: "image", repositoryId: "", exportRepositoryEnv: false, command: null,
  image: { ref: "postgres:16", args: ["-c", "log_statement=all"], ports: [{ host: 5432, container: 5432, protocol: "tcp" }] }, ...o,
});

test("a new draft is a command service with nothing in it", () => {
  const d = newDraft();
  assert.equal(d.kind, "command");
  assert.deepEqual([d.name, d.line, d.image, d.env, d.repositoryId, d.exportRepositoryEnv], ["", "", "", "", "", false]);
  assert.deepEqual([d.ports, d.args, d.mappings], [[{ value: "" }], [], [{ host: "", container: "", protocol: "tcp" }]]);
});

test("a draft from a command service holds its fields as text, ready to edit", () => {
  const d = draftFromService(service());
  assert.deepEqual([d.name, d.kind, d.line, d.subdir, d.repositoryId, d.exportRepositoryEnv, d.env], ["api", "command", "make air", "cmd/api", "r1", true, "PORT=8080"]);
  assert.deepEqual(d.ports.map((p) => p.value), ["8080", "9090"]);
});

test("a draft from an image service holds its reference, arguments and mappings", () => {
  const d = draftFromService(image());
  assert.deepEqual([d.kind, d.image, d.args.map((a) => a.value)], ["image", "postgres:16", ["-c", "log_statement=all"]]);
  assert.deepEqual(d.mappings, [{ host: "5432", container: "5432", protocol: "tcp" }]);
});

test("a command draft becomes the API's input: numbers, no image, blank port rows dropped", () => {
  const d = { ...draftFromService(service()), ports: [{ value: "8080" }, { value: "" }, { value: " 9090 " }] };
  assert.deepEqual(toInput(d), {
    name: "api", kind: "command", repository_id: "r1", export_repository_env: true, env: "PORT=8080",
    command: { line: "make air", subdir: "cmd/api", ports: [8080, 9090] }, image: null,
  });
});

test("an image draft becomes the input: no command, empty arguments and mappings dropped, protocol kept", () => {
  const d = { ...draftFromService(image()), args: [{ value: "-c" }, { value: "" }, { value: "x=1" }], mappings: [{ host: "5432", container: "5432", protocol: "tcp" }, { host: "", container: "", protocol: "udp" }, { host: "53", container: "53", protocol: "udp" }] };
  assert.deepEqual(toInput(d), {
    name: "db", kind: "image", repository_id: "", export_repository_env: false, env: "PORT=8080",
    command: null,
    image: { ref: "postgres:16", args: ["-c", "x=1"], ports: [{ host: 5432, container: 5432, protocol: "tcp" }, { host: 53, container: 53, protocol: "udp" }] },
  });
});

test("the .env export is off whenever no repository is picked", () => {
  assert.equal(toInput({ ...draftFromService(service()), repositoryId: "", exportRepositoryEnv: true }).export_repository_env, false);
});

test("a port that is not a whole number is sent as typed so the server names it", () => {
  const input = toInput({ ...draftFromService(service()), ports: [{ value: "80a" }] });
  assert.deepEqual(input.command.ports, [0]);
});

test("rows are added and removed, and the last row of a list never leaves it empty of a row to type in", () => {
  let d = newDraft();
  d = addRow(d, "ports");
  assert.equal(d.ports.length, 2);
  d = removeRow(d, "ports", 0);
  assert.equal(d.ports.length, 1);
  d = removeRow(d, "ports", 0);
  assert.deepEqual(d.ports, [{ value: "" }]);
  d = addRow(d, "args");
  assert.deepEqual(d.args, [{ value: "" }]);
  d = addRow(d, "mappings");
  assert.equal(d.mappings.length, 2);
  assert.deepEqual(d.mappings[1], { host: "", container: "", protocol: "tcp" });
});

test("changing a new draft's kind keeps what both kinds share", () => {
  const d = setKind({ ...newDraft(), name: "x", env: "A=1", repositoryId: "r1" }, "image");
  assert.deepEqual([d.kind, d.name, d.env, d.repositoryId], ["image", "x", "A=1", "r1"]);
});

test("the server's INVALID_INPUT message names its field", () => {
  assert.deepEqual(fieldOf("name must be 1 to 64 characters: lowercase letters"), { field: "name", index: -1 });
  assert.deepEqual(fieldOf("command.line is required for a command service"), { field: "line", index: -1 });
  assert.deepEqual(fieldOf("command.subdir must be relative and stay inside the repository"), { field: "subdir", index: -1 });
  assert.deepEqual(fieldOf("command.ports[1] must be a port from 1 to 65535"), { field: "ports", index: 1 });
  assert.deepEqual(fieldOf("image.ref is required for an image service"), { field: "image", index: -1 });
  assert.deepEqual(fieldOf("image.ports[0].host: host port 80 is mapped twice"), { field: "mappings", index: 0 });
  assert.deepEqual(fieldOf("env line 3 is not KEY=VALUE"), { field: "env", index: -1 });
  assert.deepEqual(fieldOf("kind must be command or image"), { field: "kind", index: -1 });
  assert.deepEqual(fieldOf("something else"), { field: "", index: -1 });
});

test("the form's title and whether it can save", () => {
  assert.equal(formTitle(null), "New service");
  assert.equal(formTitle(service()), "Edit api");
  assert.equal(canSave({ ...newDraft(), name: "" }), false);
  assert.equal(canSave({ ...newDraft(), name: "api", line: "make" }), true);
  assert.equal(canSave({ ...newDraft(), kind: "image", name: "db", image: "" }), false);
});
