import { StructuredError } from "../../../../shared/domain/errors.js";
import { mount, seededElement } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryCatalog } from "../../infrastructure/memory-gateway.js";
import { makeWorld } from "../../testing/fixtures.js";
import { agentsPage } from "./agentsPage.js";

file("agents/presentation/components/mcpEditor");

// A fake token, shaped like a Linear key.
const LIN = "lin_api_" + "8Hk2xQ9vT4mZpR7wY1cD";

function fakeTimers() {
  const pending = new Map();
  let id = 0;
  return {
    setTimeout: (fn) => { pending.set(++id, fn); return id; },
    clearTimeout: (n) => { pending.delete(n); },
    async fire() {
      const fns = [...pending.values()];
      pending.clear();
      for (const fn of fns) await fn();
    },
    get waiting() { return pending.size; },
  };
}
const settle = () => new Promise((r) => setTimeout(r, 0));

/** The page at `path`, over the memory catalog with two projects. */
async function open(path, { search = "", hash = "", probe, env } = {}) {
  const world = makeWorld();
  world.projects.p2 = [];
  world.projectNames.p2 = "billing-api";
  world.catalog.agents[1].description = "Builds the API.";
  if (env) Object.assign(world.env, env);
  const { gateway, server } = memoryCatalog(world);
  if (probe) {
    const real = gateway.testMcpServer;
    gateway.testMcpServer = (s) => probe(s, real);
  }
  const timers = fakeTimers();
  const pushed = [];
  const location = { pathname: path, search, hash };
  const history = {
    pushState: (_s, _t, url) => {
      pushed.push(url);
      const [rest, h = ""] = url.split("#");
      const [p, q = ""] = rest.split("?");
      location.pathname = p;
      location.search = q ? `?${q}` : "";
      location.hash = h ? `#${h}` : "";
    },
    replaceState: () => {},
  };
  const seed = await server.api.get("/get_agent_catalog");
  const m = mount(agentsPage({ gateway, history, location, timers }), { el: seededElement(seed) });
  await m.instance.init();
  await settle();
  return { ...m, gateway, server, timers, pushed, location, page: m.instance };
}

const ev = (dataset = {}, extra = {}) => ({ currentTarget: { dataset, ...extra }, preventDefault() {}, ...extra });
const saves = (server, dry) => server.requests.filter(([m, p, b]) => m === "POST" && p === "/save_catalog_mcp_server" && Boolean(b.dry_run) === dry);
const wire = (server) => JSON.stringify(server.requests);
const rowOf = (page, section) => page.mfDraft[section].at(-1);

/** Types into the form the way the page's handlers hear it. */
const typeName = (page, v) => { page.mfDraft.name = v; page.mfNameInput(); };
const typeCommand = (page, v) => { page.mfDraft.command = v; page.mfCommandInput(); };
const typeUrl = (page, v) => { page.mfDraft.url = v; page.mfChanged(); };
const addRow = (page, section) => { page.mfAddRow(ev({ section })); return rowOf(page, section); };
const setValue = (page, section, row, value) => page.mfRowInput(ev({ section, id: String(row.id), field: "value" }, { value }));
const setKey = (page, section, row, value) => page.mfRowInput(ev({ section, id: String(row.id), field: "key" }, { value }));

test("/agents/mcp/new is a full page: no tabs, a local command by default, nothing written, Save off", async () => {
  const { page } = await open("/agents/mcp/new");
  assert.equal(page.mfOpen, true);
  assert.equal(page.mfDrawer, false);
  assert.equal(page.fullPage, true);
  assert.equal(page.mfTitle, "New MCP server");
  assert.equal(page.saveButtonLabel, "Add server");
  assert.equal(page.mfDraft.transport, "stdio");
  assert.equal(page.mfLocal, true);
  assert.equal(page.sv.canSave, false);
  assert.equal(page.sv.footer, "Nothing is written until you add the server.");
  assert.equal(page.fieldErrors.name, "", "a new form does not scold");
  assert.deepEqual(page.mfAgentRows.map((r) => r.name), ["backend", "ux-designer"]);
  assert.deepEqual(page.mfProjectRows.map((r) => r.name), ["billing-api", "harness"]);
  assert.equal(page.mfWho.unused, true);
});

test("choosing Remote URL shows the URL and headers, not the command", async () => {
  const { page } = await open("/agents/mcp/new");
  page.mfPickTransport(ev({ transport: "http" }));
  assert.equal(page.mfLocal, false);
  assert.equal(page.mfRemote, true);
  assert.equal(page.mfDraft.transport, "http");
  assert.equal(page.mfLocalPressed, "false");
  assert.equal(page.mfRemotePressed, "true");
  page.mfPickTransport(ev({ transport: "stdio" }));
  assert.equal(page.mfDraft.transport, "stdio");
});

test("pasting a command line into Command splits it into the command and one argument per row", async () => {
  const { page } = await open("/agents/mcp/new");
  const pasted = { clipboardData: { getData: () => `npx -y @linear/mcp-server --label "two words"` }, defaultPrevented: false, preventDefault() { this.defaultPrevented = true; } };
  page.mfCommandPaste(pasted);
  assert.equal(pasted.defaultPrevented, true);
  assert.equal(page.mfDraft.command, "npx");
  assert.deepEqual(page.mfDraft.args.map((a) => a.value), ["-y", "@linear/mcp-server", "--label", "two words"]);
  assert.equal(page.mfNote, "Split into a command and 4 arguments.");

  const word = { clipboardData: { getData: () => "npx" }, defaultPrevented: false, preventDefault() { this.defaultPrevented = true; } };
  page.mfCommandPaste(word);
  assert.equal(word.defaultPrevented, false, "one word pastes as usual");

  typeCommand(page, "uvx mcp-server-git");
  assert.equal(page.mfDraft.command, "uvx", "a line left in the field when it loses focus splits too");
  assert.deepEqual(page.mfDraft.args.map((a) => a.value), ["mcp-server-git"]);
});

test("a typed name and command ask the server for a preview after a pause, and Add server writes it", async () => {
  const { page, timers, server, pushed } = await open("/agents/mcp/new");
  typeName(page, "playwright");
  typeCommand(page, "npx");
  assert.equal(page.sv.canSave, true);
  assert.equal(saves(server, true).length, 0, "nothing is asked while typing");
  await timers.fire();
  assert.equal(saves(server, true).length, 1);
  const sent = saves(server, true)[0][2];
  assert.deepEqual(sent.server, { name: "playwright", transport: "stdio", command: "npx", args: [], env: {}, url: "", headers: {} });
  assert.deepEqual([sent.agents, sent.projects], [[], []]);
  assert.deepEqual(sent.base_versions, { "agents.json": "sha256:a1", "mcp-servers.json": "sha256:m1" });
  assert.deepEqual(page.mfPreviewFiles.map((f) => f.path), ["mcp-servers.json"]);
  assert.equal(page.mfHasPreview, true);
  assert.equal(page.sv.footer, "2 unsaved changes · writes mcp-servers.json. Unused: no agent and no project gets it yet.");

  await page.saveEditor();
  assert.equal(saves(server, false).length, 1);
  assert.deepEqual(pushed, ["/agents/mcp/playwright"]);
  assert.equal(page.toast.hasUndo, true);
  assert.ok(page.catalog.mcpServers.some((s) => s.name === "playwright"));
  assert.equal(page.mfOpen, false);
});

test("a token typed as plain text warns, blocks the preview, the save and the test, and travels nowhere", async () => {
  const { page, timers, server } = await open("/agents/mcp/new");
  typeName(page, "linear");
  page.mfPickTransport(ev({ transport: "http" }));
  typeUrl(page, "https://mcp.linear.app/mcp");
  const row = addRow(page, "headers");
  setKey(page, "headers", row, "Authorization");
  setValue(page, "headers", row, `Bearer ${LIN}`);
  await timers.fire();

  assert.equal(row.mode, "text", "the row flips to Plain text");
  assert.equal(page.sv.canSave, false);
  assert.equal(page.sv.footer, "Settle the token warning to add the server.");
  assert.equal(page.mfHeaderRows[0].secret.state, "warn");
  assert.equal(page.mfHeaderRows[0].secret.title, "This looks like a Linear API key.");
  assert.equal(page.mfHeaderRows[0].secret.useLabel, "Use ${LINEAR_API_KEY} instead");
  assert.equal(page.mfHeaderRows[0].inputType, "password", "the token is masked in its own field");
  assert.equal(page.mfHeaderRows[0].invalid, "true");
  assert.equal(page.mfTestable, false);
  await page.mfTest();
  assert.equal(server.requests.filter(([, p]) => p === "/test_catalog_mcp_server").length, 0);
  assert.equal(saves(server, true).length, 0);
  assert.ok(!wire(server).includes(LIN), "the value was sent nowhere");
  assert.ok(!JSON.stringify({ sv: page.sv, errors: page.fieldErrors, rows: page.mfHeaderRows.map((r) => ({ ...r, value: undefined })) }).includes(LIN), "nothing the page shows holds it");
});

test("Use ${VAR} instead swaps the token for a reference, says where to set it, and the value is gone", async () => {
  const { page, timers, server } = await open("/agents/mcp/new");
  typeName(page, "linear");
  page.mfPickTransport(ev({ transport: "http" }));
  typeUrl(page, "https://mcp.linear.app/mcp");
  const row = addRow(page, "headers");
  setKey(page, "headers", row, "Authorization");
  setValue(page, "headers", row, `Bearer ${LIN}`);
  page.mfUseVariable(ev({ section: "headers", id: String(row.id) }));

  assert.equal(row.value, "Bearer ${LINEAR_API_KEY}");
  assert.equal(row.mode, "env");
  assert.equal(page.mfHeaderRows[0].secret, null);
  assert.equal(page.mfHeaderRows[0].used, "The pasted key is gone from this form. The harness does not store it. Set LINEAR_API_KEY to that key in the harness server's environment, then restart the server if it is not read live.");
  assert.equal(page.sv.canSave, true);
  await timers.fire();
  assert.ok(!wire(server).includes(LIN), "the token never left the form");
  assert.equal(saves(server, true)[0][2].server.headers.Authorization, "Bearer ${LINEAR_API_KEY}");
  await settle();
  assert.deepEqual(page.mfEnvRefs.map((r) => [r.label, r.state]), [["${LINEAR_API_KEY}", "not set"]]);
});

test("Keep as text writes the value, says so, hides it in the preview, and sends it only in its own field", async () => {
  const { page, timers, server } = await open("/agents/mcp/new");
  typeName(page, "linear");
  page.mfPickTransport(ev({ transport: "http" }));
  typeUrl(page, "https://mcp.linear.app/mcp");
  const row = addRow(page, "headers");
  setKey(page, "headers", row, "Authorization");
  setValue(page, "headers", row, `Bearer ${LIN}`);
  page.mfKeepAsText(ev({ section: "headers", id: String(row.id) }));

  assert.equal(page.mfHeaderRows[0].secret.state, "kept");
  assert.equal(page.mfHeaderRows[0].secret.title, "Kept as plain text.");
  assert.equal(page.mfHeaderRows[0].secret.detail, "The key is written as it is into mcp-servers.json in the data directory.");
  assert.equal(page.sv.canSave, true);
  await timers.fire();
  const sent = saves(server, true)[0][2];
  assert.equal(sent.server.headers.Authorization, `Bearer ${LIN}`);
  assert.equal(JSON.stringify(sent).split(LIN).length, 2, "once, in the header it was typed in");
  assert.ok(!JSON.stringify(page.mfPreviewFiles).includes(LIN), "the preview hides it");
  assert.ok(JSON.stringify(page.mfPreviewFiles).includes("••••••"));
  assert.ok(!JSON.stringify(page.sv).includes(LIN));

  await page.saveEditor();
  assert.equal(saves(server, false)[0][2].server.headers.Authorization, `Bearer ${LIN}`);
  assert.ok(!JSON.stringify({ toast: page.toast }).includes(LIN));
});

test("every ${VAR} of the form is checked on the server and shows set or not set; Check again asks once more", async () => {
  const { page, timers, server } = await open("/agents/mcp/new", { env: { SET_ONE: true } });
  typeName(page, "mixed");
  typeCommand(page, "run-${SET_ONE}");
  const env = addRow(page, "env");
  setKey(page, "env", env, "TOKEN");
  setValue(page, "env", env, "${UNSET_ONE}");
  await timers.fire();
  await settle();
  const asked = () => server.requests.filter(([, p]) => p === "/check_env_vars").map(([, , q]) => q.names);
  assert.deepEqual(asked(), ["SET_ONE,UNSET_ONE"]);
  assert.deepEqual(page.mfEnvRefs.map((r) => [r.label, r.state, r.tone]), [["${SET_ONE}", "set", "success"], ["${UNSET_ONE}", "not set", "attention"]]);
  assert.equal(page.mfEnvUnset, true);
  assert.equal(page.mfEnvUnsetText, "${UNSET_ONE} is not set in the harness server's environment. Saving works; the server fails to start until it is set.");

  setValue(page, "env", env, "${UNSET_ONE} and ${SET_ONE}");
  await timers.fire();
  assert.deepEqual(asked(), ["SET_ONE,UNSET_ONE"], "a name already known is not asked again");

  server.requests.length = 0;
  await page.mfCheckAgain();
  assert.deepEqual(asked(), ["SET_ONE,UNSET_ONE"]);
});

test("under From env a bare variable name becomes its ${reference}", async () => {
  const { page } = await open("/agents/mcp/new");
  const env = addRow(page, "env");
  setKey(page, "env", env, "TOKEN");
  setValue(page, "env", env, "GITHUB_TOKEN");
  page.mfRowChange(ev({ section: "env", id: String(env.id), field: "value" }, { value: "GITHUB_TOKEN" }));
  assert.equal(env.value, "${GITHUB_TOKEN}");
  page.mfRowMode(ev({ section: "env", id: String(env.id) }, { value: "text" }));
  assert.equal(env.mode, "text");
  setValue(page, "env", env, "plain");
  page.mfRowChange(ev({ section: "env", id: String(env.id), field: "value" }, { value: "plain" }));
  assert.equal(env.value, "plain");
});

test("Test connection shows a waiting state for up to 20 seconds, then the tools", async () => {
  let release;
  let started = 0;
  const { page, timers } = await open("/agents/mcp/new", { probe: () => { started++; return new Promise((r) => { release = r; }); } });
  typeName(page, "playwright");
  typeCommand(page, "npx");
  assert.equal(page.mfTestable, true);
  const running = page.mfTest();
  assert.equal(page.mfProbeRunning, true);
  assert.equal(page.mfTestLabel, "Testing…");
  assert.equal(page.mfProbeWait, "Connecting… 0 s. The test stops after 20 s.");
  assert.equal(page.mfTestBusy, "true");
  await page.mfTest();
  assert.equal(started, 1, "a second click does not start a second test");
  await timers.fire();
  assert.equal(page.mfProbeWait, "Connecting… 1 s. The test stops after 20 s.");
  release({ status: "ok", tools: [{ name: "browser_click", description: "" }, { name: "browser_navigate", description: "" }], error: "", hint: "" });
  await running;
  assert.equal(page.mfProbeRunning, false);
  assert.equal(page.mfProbeView.phase, "ok");
  assert.equal(page.mfProbeView.title, "Connected. 2 tools:");
  assert.deepEqual(page.mfProbeView.tools, ["browser_click", "browser_navigate"]);
  assert.equal(page.mfTestLabel, "Test connection");
  assert.equal(timers.waiting, 0, "the ticking stops with the test");
});

test("a failed test shows the cause and the hint, saving stays possible, and an edit marks the answer old", async () => {
  const { page, server } = await open("/agents/mcp/new");
  typeName(page, "ghost");
  typeCommand(page, "missing-cmd");
  await page.mfTest();
  assert.equal(page.mfProbeView.phase, "error");
  assert.equal(page.mfProbeView.title, "executable not found");
  assert.equal(page.mfProbeView.hint, "Install it first.");
  assert.equal(page.sv.canSave, true, "a failed test does not block the save");
  assert.equal(page.mfProbeStale, null);
  typeCommand(page, "other-cmd");
  assert.equal(page.mfProbeStale, "true");
  const sent = server.requests.find(([, p]) => p === "/test_catalog_mcp_server")[2];
  assert.deepEqual(sent, { server: { name: "ghost", transport: "stdio", command: "missing-cmd", args: [], env: {}, url: "", headers: {} } });
});

test("a test the server cannot run is shown as a failure in its words", async () => {
  const { page } = await open("/agents/mcp/new", { probe: async () => { throw Object.assign(new Error("testing MCP servers is not available"), { code: "UNAVAILABLE" }); } });
  typeName(page, "x");
  typeCommand(page, "npx");
  await page.mfTest();
  assert.equal(page.mfProbeView.phase, "error");
  assert.equal(page.mfProbeView.title, "testing MCP servers is not available");
});

test("Who gets it: ticking agents and projects is what the save sends, and the footer says how many", async () => {
  const { page, timers, server } = await open("/agents/mcp/new");
  typeName(page, "playwright");
  typeCommand(page, "npx");
  page.mfToggleAgent(ev({ name: "backend" }));
  page.mfToggleProject(ev({ id: "p2" }));
  assert.equal(page.mfWhoText, "Reaches 1 agent and 1 project, from their next session. Running sessions keep what they started with.");
  assert.equal(page.mfWhoTone, "");
  await timers.fire();
  const sent = saves(server, true)[0][2];
  assert.deepEqual([sent.agents, sent.projects], [["backend"], ["p2"]]);
  assert.deepEqual(page.mfPreviewFiles.map((f) => f.path), ["mcp-servers.json", "agents.json", "project “billing-api”"]);
  assert.equal(page.mfPreviewCount, "2 files · 1 project list");
  assert.equal(page.sv.footer, "4 unsaved changes · writes mcp-servers.json, agents.json. Reaches 1 agent and 1 project from their next session.");
  page.mfToggleAgent(ev({ name: "backend" }));
  page.mfToggleProject(ev({ id: "p2" }));
  assert.equal(page.mfWhoTone, "attention");
  assert.equal(page.mfWho.unused, true);
});

test("a name the catalog has is refused at the field, with a link and a name to try, and the field points at its error", async () => {
  const { page } = await open("/agents/mcp/new");
  typeName(page, "github");
  typeCommand(page, "npx");
  assert.equal(page.fieldErrors.name, "A server named github already exists.");
  assert.equal(page.mfTakenHref, "/agents/mcp/github");
  assert.equal(page.mfSuggestion, "github-2");
  assert.equal(page.mfNameInvalid, true);
  assert.equal(page.mfNameDescribedBy, "mf-name-hint mf-name-error");
  assert.equal(page.sv.canSave, false);
  assert.equal(page.sv.footer, "Fix 1 field to add the server.");
  page.mfUseSuggestion();
  assert.equal(page.mfDraft.name, "github-2");
  assert.equal(page.mfNameDescribedBy, "mf-name-hint");
  assert.equal(page.sv.canSave, true);
});

test("the server's NAME_TAKEN and INVALID_INPUT land on their fields and are linked by aria-describedby", async () => {
  const { page, timers, server } = await open("/agents/mcp/new");
  typeName(page, "fresh");
  typeCommand(page, "npx");
  await server.api.post("/save_catalog_mcp_server", { server: { name: "fresh", transport: "stdio", command: "x", args: [], env: {}, url: "", headers: {} }, agents: [], projects: [] });
  await timers.fire();
  assert.equal(page.fieldErrors.name, "A server named fresh already exists.");
  assert.equal(page.mfSuggestion, "fresh-2");
  assert.equal(page.mfNameDescribedBy, "mf-name-hint mf-name-error");
});

test("a server's own INVALID_INPUT shows at its field", async () => {
  const { page, timers, server } = await open("/agents/mcp/new");
  const real = server.api.post;
  server.api.post = async (path, body) => {
    if (path === "/save_catalog_mcp_server") throw new StructuredError("INVALID_INPUT", "an http server needs a url", 400, { field: "url" });
    return real(path, body);
  };
  typeName(page, "remote");
  page.mfPickTransport(ev({ transport: "http" }));
  typeUrl(page, "https://ok.test");
  await timers.fire();
  assert.equal(page.fieldErrors.url, "an http server needs a url");
  assert.equal(page.mfUrlInvalid, true);
  assert.equal(page.mfUrlDescribedBy, "mf-url-hint mf-url-error");
});

test("editing opens with the server's fields and who has it; a rename says what it rewrites and sends previous_name", async () => {
  const { page, timers, server } = await open("/agents/mcp/github/edit");
  assert.equal(page.mfTitle, "Edit github");
  assert.equal(page.saveButtonLabel, "Save server");
  assert.equal(page.mfDraft.command, "npx");
  assert.deepEqual(page.mfArgRows.map((r) => r.value), ["-y", "gh-mcp"]);
  assert.deepEqual(page.mfEnvRows.map((r) => [r.key, r.value, r.isEnv]), [["TOKEN", "${GITHUB_TOKEN}", true]]);
  assert.deepEqual(page.mfAgentRows.filter((r) => r.checked).map((r) => r.name), ["ux-designer"]);
  assert.deepEqual(page.mfProjectRows.filter((r) => r.checked).map((r) => r.name), ["harness"]);
  assert.equal(page.mfRenameShown, true);
  assert.equal(page.mfRenameText, "Renaming rewrites the name in 1 agent and 1 project. One save changes them all, or none.");
  assert.equal(page.sv.canSave, false);

  typeName(page, "gh");
  await timers.fire();
  assert.equal(saves(server, true)[0][2].previous_name, "github");
  await page.saveEditor();
  assert.ok(page.catalog.mcpServers.some((s) => s.name === "gh"));
});

test("a stored plain-text token opens as kept, with the way out beside it", async () => {
  const world = makeWorld();
  world.catalog.mcp_servers.push({ name: "stored", transport: "http", command: "", args: [], env: {}, url: "https://x.test", headers: { Authorization: `Bearer ${LIN}` }, env_refs: [], agents: [], projects: [], problems: [] });
  const { gateway, server } = memoryCatalog(world);
  const seed = await server.api.get("/get_agent_catalog");
  const m = mount(agentsPage({ gateway, history: { pushState() {} }, location: { pathname: "/agents/mcp/stored/edit", search: "" }, timers: fakeTimers() }), { el: seededElement(seed) });
  await m.instance.init();
  await settle();
  const page = m.instance;
  assert.equal(page.mfHeaderRows[0].secret.state, "kept");
  assert.equal(page.mfHeaderRows[0].inputType, "password");
  assert.equal(page.mfHeaderRows[0].secret.useLabel, "Use ${STORED_API_KEY} instead");
  assert.equal(page.sv.dirty, false, "nothing to settle: the choice was made when it was saved");
});

test("from another page, a save goes back with the new server named in the address; Cancel goes back without it", async () => {
  const a = await open("/agents/mcp/new", { search: "?return=%2Fagents%2Fnew%23mcp" });
  assert.equal(a.page.mfBackLabel, "← Back to the agent");
  typeName(a.page, "playwright");
  typeCommand(a.page, "npx");
  await a.page.saveEditor();
  assert.deepEqual(a.pushed, ["/agents/new?added=playwright#mcp"]);
  assert.equal(a.page.agentEditing, true);
  assert.equal(a.page.mfOpen, false);

  const b = await open("/agents/mcp/new", { search: "?return=%2Fagents%2Fux-designer%2Fedit%23mcp" });
  b.page.cancelEditor({ preventDefault() {} });
  assert.deepEqual(b.pushed, ["/agents/ux-designer/edit#mcp"]);

  const c = await open("/agents/mcp/new", { search: "?return=https%3A%2F%2Fevil.test%2F" });
  assert.equal(c.page.mfBack, "");
  c.page.cancelEditor({ preventDefault() {} });
  assert.deepEqual(c.pushed, ["/agents/mcp"]);
});

test("the agent editor ticks the server named in ?added when the address ends in #mcp, and adds no skill", async () => {
  const { page } = await open("/agents/new", { search: "?added=github", hash: "#mcp" });
  assert.deepEqual(page.agentDraft.mcpServers, ["github"]);
  assert.deepEqual(page.agentDraft.entries, []);
  const skill = await open("/agents/new", { search: "?added=tdd", hash: "#skills" });
  assert.deepEqual(skill.page.agentDraft.entries, ["tdd"]);
  assert.deepEqual(skill.page.agentDraft.mcpServers, []);
});

test("in the agent editor, New MCP server… opens a drawer over the edit, and the form is unattached", async () => {
  const { page } = await open("/agents/ux-designer/edit");
  page.agentDraft.description = "Designs screens, edited.";
  page.agentChanged();
  page.mfOpenDrawer();
  assert.equal(page.mfOpen, true);
  assert.equal(page.mfDrawer, true);
  assert.equal(page.mfMode, "drawer");
  assert.equal(page.mfRole, "dialog");
  assert.equal(page.agentEditing, true, "the agent editor is still there under it");
  assert.equal(page.mfAgentInert, true);
  assert.equal(page.mfAttachable, false, "Who gets it is not asked: the agent gets it when it is saved");
  assert.equal(page.mfTitle, "New MCP server");
  assert.equal(page.sv.footer, "Nothing is written until you add the server.");
  assert.equal(page.agentDraft.description, "Designs screens, edited.", "the half-done edit is kept");
});

test("saving the drawer's server writes it alone, ticks it in the agent editor, and keeps the edit", async () => {
  const { page, server, timers } = await open("/agents/backend/edit");
  page.agentDraft.description = "Builds the API, edited.";
  page.agentChanged();
  page.mfOpenDrawer();
  typeName(page, "playwright");
  typeCommand(page, "npx");
  page.mfToggleAgent(ev({ name: "backend" }));
  await timers.fire();
  assert.deepEqual([saves(server, true).at(-1)[2].agents, saves(server, true).at(-1)[2].projects], [[], []], "unattached, whatever was ticked");
  await page.saveEditor();
  assert.deepEqual([saves(server, false)[0][2].agents, saves(server, false)[0][2].projects], [[], []]);
  assert.equal(page.mfOpen, false);
  assert.equal(page.mfDrawer, false);
  assert.deepEqual(page.agentDraft.mcpServers, ["playwright"], "ticked in the editor");
  assert.equal(page.agentDraft.description, "Builds the API, edited.", "the edit survived");
  assert.equal(page.agentEditing, true);
  assert.equal(page.toast.hasUndo, true);
  assert.deepEqual(page.mcpRowsChoice.filter((r) => r.checked).map((r) => r.name), ["playwright"]);
  assert.equal(page.sv.dirty, true, "the agent is still unsaved: the agent gets the server when it is saved");
  assert.ok(!page.catalog.agents.find((a) => a.name === "backend").mcpServers.includes("playwright"));
});

test("Cancel on a clean drawer closes it; on a dirty one it asks, and Discard keeps the agent edit", async () => {
  const { page } = await open("/agents/backend/edit");
  page.agentDraft.description = "Kept.";
  page.agentChanged();
  page.mfOpenDrawer();
  page.cancelEditor({ preventDefault() {} });
  assert.equal(page.mfOpen, false, "nothing to lose");
  assert.equal(page.agentDraft.description, "Kept.");

  page.mfOpenDrawer();
  typeName(page, "half-done");
  page.cancelEditor({ preventDefault() {} });
  assert.equal(page.mfOpen, true);
  assert.equal(page.leaving !== null, true);
  page.keepEditing();
  assert.equal(page.mfDraft.name, "half-done");
  page.cancelEditor({ preventDefault() {} });
  page.discardAndLeave();
  assert.equal(page.mfOpen, false);
  assert.equal(page.agentEditing, true, "only the drawer went");
  assert.equal(page.agentDraft.description, "Kept.");
});

test("Escape closes the drawer the way Cancel does, and the full page ignores it", async () => {
  const { page } = await open("/agents/backend/edit");
  page.mfOpenDrawer();
  page.mfEscape({ preventDefault() {} });
  assert.equal(page.mfOpen, false);
  const full = await open("/agents/mcp/new");
  full.page.mfEscape({ preventDefault() {} });
  assert.equal(full.page.mfOpen, true);
});

test("an outside change blocks the save and shows it; Overwrite writes again with force", async () => {
  const { page, server, timers } = await open("/agents/mcp/github/edit");
  typeCommand(page, "pnpx");
  await timers.fire();
  server.editOnDisk("mcp-servers.json");
  await page.saveEditor();
  assert.equal(page.sv.conflict, true);
  assert.equal(page.sv.footer, "Saving is off until you choose Reload or Overwrite.");
  assert.equal(page.mfDraft.command, "pnpx", "the edit is kept");
  await page.overwrite();
  assert.equal(saves(server, false).at(-1)[2].force, true);
});

test("a server the catalog lacks has no form", async () => {
  const { page } = await open("/agents/mcp/ghost/edit");
  assert.equal(page.mfOpen, true);
  assert.equal(page.mfMissing, true);
  assert.equal(page.mfReady, false);
});

test("duplicating copies the fields into a form with no name and nobody picked", async () => {
  const { page } = await open("/agents/mcp/new", { search: "?from=github" });
  assert.equal(page.mfDraft.name, "");
  assert.equal(page.mfDraft.command, "npx");
  assert.deepEqual(page.mfDraft.agents, []);
  assert.equal(page.sv.dirty, true);
});

test("leaving the form with unsaved changes is guarded; closing the page's form leaves the tabs", async () => {
  const { page } = await open("/agents/mcp/new");
  const e = { defaultPrevented: false, returnValue: undefined, preventDefault() { this.defaultPrevented = true; } };
  page.beforeUnload(e);
  assert.equal(e.defaultPrevented, false);
  typeName(page, "dirty");
  page.cancelEditor({ preventDefault() {} });
  assert.equal(page.leaving !== null, true);
  page.discardAndLeave();
  assert.equal(page.mfOpen, false);
  assert.equal(page.tab, "mcp");
  assert.equal(page.fullPage, false);
});
