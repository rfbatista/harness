import { assert, file, test } from "../../../shared/testing/test.js";
import { toCatalog } from "../infrastructure/dto.js";
import { catalogDTO } from "../testing/fixtures.js";
import { mcpDetail, mcpFooter, mcpRows, noOwnServers, varNames } from "./mcp.js";

file("agents/domain/mcp");

const catalogWith = (servers) => {
  const dto = catalogDTO();
  dto.mcp_servers = servers;
  return toCatalog(dto);
};
const server = (o) => ({ name: "s", transport: "stdio", command: "npx", args: [], env: {}, url: "", headers: {}, env_refs: [], agents: [], projects: [], problems: [], ...o });

test("${VAR} references come out of text in order, once each", () => {
  assert.deepEqual(varNames("Bearer ${TOKEN} and ${TOKEN} and ${OTHER_1}"), ["TOKEN", "OTHER_1"]);
  assert.deepEqual(varNames("no refs, $HOME and ${ not one } and ${1BAD}"), []);
  assert.deepEqual(varNames("${A:-default}"), ["A"]);
});

test("a server's references are found in its env, args and headers", () => {
  const d = mcpDetail(catalogWith([server({ args: ["--key", "${ARG_KEY}"], env: { TOKEN: "${ENV_TOKEN}" }, headers: { Authorization: "Bearer ${HDR_TOKEN}" } })]), "s", {});
  assert.deepEqual(d.refs.map((r) => r.name), ["ARG_KEY", "ENV_TOKEN", "HDR_TOKEN"]);
});

test("each env and header entry shows its template with set or not set, and a plain value is never shown", () => {
  const c = catalogWith([server({
    env: { TOKEN: "${A}", LEVEL: "hunter2-secret" }, headers: {}, env_refs: [{ name: "A", set: true }],
  })]);
  const d = mcpDetail(c, "s", {});
  assert.equal(d.entriesLabel, "Env");
  assert.deepEqual(d.entries.map((e) => [e.key, e.text, e.plain]), [["TOKEN", "${A}", false], ["LEVEL", "plain text, hidden", true]]);
  assert.equal(d.entries[0].refs[0].state, "set");
  assert.ok(!JSON.stringify(d).includes("hunter2-secret"), "a literal value must not reach the view");
});

test("an http server shows its URL and its headers", () => {
  const d = mcpDetail(catalogWith([server({ transport: "http", command: "", url: "https://x.dev/mcp", headers: { Authorization: "Bearer ${T}" }, env_refs: [{ name: "T", set: false }] })]), "s", {});
  assert.equal(d.targetLabel, "URL");
  assert.equal(d.target, "https://x.dev/mcp");
  assert.equal(d.entriesLabel, "Headers");
  assert.deepEqual([d.entries[0].key, d.entries[0].text], ["Authorization", "Bearer ${T}"]);
});

test("a stdio server shows its command with its arguments", () => {
  const d = mcpDetail(catalogWith([server({ args: ["-y", "gh-mcp"] })]), "s", {});
  assert.equal(d.targetLabel, "Command");
  assert.equal(d.target, "npx -y gh-mcp");
});

test("the banner names the variables that are not set; a re-check overrides what the catalog said", () => {
  const c = catalogWith([server({ env: { A: "${A}", B: "${B}" }, env_refs: [{ name: "A", set: false }, { name: "B", set: true }] })]);
  assert.deepEqual(mcpDetail(c, "s", {}).unset, ["A"]);
  const after = mcpDetail(c, "s", { A: true, B: false });
  assert.deepEqual(after.unset, ["B"]);
  assert.equal(after.entries[0].refs[0].state, "set");
  assert.equal(after.entries[1].refs[0].state, "not set");
});

test("who gets it: agents and projects with their settings links, or Unused", () => {
  const used = mcpDetail(catalogWith([server({ agents: ["ux"], projects: [{ id: "p 1", name: "harness" }] })]), "s", {});
  assert.deepEqual(used.agents, [{ name: "ux", href: "/agents/ux" }]);
  assert.deepEqual(used.projects, [{ name: "harness", href: "/projects/p%201/settings" }]);
  assert.equal(used.unused, false);
  assert.equal(mcpDetail(catalogWith([server({})]), "s", {}).unused, true);
});

test("the rows' filters: stdio, http, Needs a secret, Unused", () => {
  const c = catalogWith([
    server({ name: "a", env_refs: [{ name: "X", set: false }], agents: ["ux"], problems: [{ code: "SECRET_NOT_SET", message: "X is not set" }] }),
    server({ name: "b", transport: "http", url: "https://b.dev", agents: ["ux"] }),
    server({ name: "c" }),
  ]);
  const names = (filter) => mcpRows(c, { filter }).map((r) => r.name);
  assert.deepEqual(names("stdio"), ["a", "c"]);
  assert.deepEqual(names("http"), ["b"]);
  assert.deepEqual(names("secret"), ["a"]);
  assert.deepEqual(names("unused"), ["c"]);
  assert.equal(mcpRows(c).find((r) => r.name === "a").badge.label, "secret not set");
  assert.equal(mcpRows(c).find((r) => r.name === "c").meta, "stdio · Unused");
  assert.equal(mcpRows(c).find((r) => r.name === "b").description, "https://b.dev");
  assert.deepEqual(mcpRows(c, { query: "b.dev" }).map((r) => r.name), ["b"]);
});

test("the footer names mcp-servers.json", () => {
  assert.equal(mcpFooter(catalogWith([server({ name: "a" }), server({ name: "b" })])), "mcp-servers.json · 2 servers");
});

const builtIn = (o) => server({
  name: "services", transport: "built_in", command: "", built_in: true,
  tools: ["list_services", "start_service"], agents: ["ux"], projects: [{ id: "p1", name: "harness" }], ...o,
});

test("the built-in services server is read with its tools and the built-in mark", () => {
  const c = catalogWith([builtIn(), server({ name: "plain" })]);
  const [b, p] = c.mcpServers;
  assert.equal(b.builtIn, true);
  assert.deepEqual(b.tools, ["list_services", "start_service"]);
  assert.equal(p.builtIn, false);
  assert.deepEqual(p.tools, []);
});

test("a built-in row says so, and an ordinary row does not", () => {
  const rows = mcpRows(catalogWith([builtIn(), server({ name: "plain", agents: ["ux"] })]));
  const b = rows.find((r) => r.name === "services");
  assert.equal(b.builtIn, true);
  assert.equal(b.meta, "1 agent · 1 project");
  assert.equal(b.description, "list_services, start_service");
  assert.equal(rows.find((r) => r.name === "plain").builtIn, false);
});

test("a built-in server is neither stdio nor http, and is never Unused", () => {
  const c = catalogWith([builtIn(), builtIn({ name: "x", agents: [], projects: [] }), server({ name: "own" })]);
  assert.deepEqual(mcpRows(c, { filter: "stdio" }).map((r) => r.name), ["own"]);
  assert.deepEqual(mcpRows(c, { filter: "unused" }).map((r) => r.name), ["own"]);
  assert.equal(mcpDetail(c, "x", {}).unused, false);
  assert.equal(mcpRows(c).find((r) => r.name === "x").meta, "");
});

test("a built-in detail lists its tools, and has no edit, duplicate, delete or check", () => {
  const d = mcpDetail(catalogWith([builtIn()]), "services", {});
  assert.equal(d.builtIn, true);
  assert.deepEqual(d.tools, ["list_services", "start_service"]);
  assert.equal(d.targetLabel, "Tools");
  assert.equal(d.target, "list_services, start_service");
  assert.equal(d.editHref, "");
  assert.equal(d.duplicateHref, "");
  assert.equal(d.entries.length, 0);
  assert.deepEqual(d.agents, [{ name: "ux", href: "/agents/ux" }]);
});

test("an ordinary detail is not built-in and keeps its edit link", () => {
  const d = mcpDetail(catalogWith([server({})]), "s", {});
  assert.equal(d.builtIn, false);
  assert.equal(d.editHref, "/agents/mcp/s/edit");
});

test("the footer counts the file's servers, not the built-in one", () => {
  assert.equal(mcpFooter(catalogWith([builtIn(), server({ name: "a" }), server({ name: "b" })])), "mcp-servers.json · 2 servers");
  assert.equal(mcpFooter(catalogWith([builtIn()])), "mcp-servers.json · 0 servers");
});

test("a catalog with only built-in servers has none of the person's own", () => {
  assert.equal(noOwnServers(catalogWith([])), true);
  assert.equal(noOwnServers(catalogWith([builtIn()])), true);
  assert.equal(noOwnServers(catalogWith([builtIn(), server({})])), false);
});
