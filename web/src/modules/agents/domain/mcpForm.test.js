import { StructuredError } from "../../../shared/domain/errors.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import {
  addRow, checkMcpDraft, detectToken, hideSecrets, mcpChanges, mcpDraft, mcpFieldErrors, mcpPreviewModel, mcpRefs, mcpSave,
  probeView, referenceName, removeRow, rename, setMode, splitCommandLine, toReference, useReference, whoGetsIt,
} from "./mcpForm.js";
import { describeRefusal } from "./refusals.js";

file("agents/domain/mcpForm");

// Fake tokens: shaped like the real ones, worth nothing.
const GH = "ghp_" + "a1B2c3D4e5F6g7H8i9J0k1L2m3N4o5P6q7R8";
const SK = "sk-" + "proj-9fQ2LxTz81KdVb3NmWc7Ye";
const LIN = "lin_api_" + "8Hk2xQ9vT4mZpR7wY1cD";
const JWT = "eyJhbGciOiJIUzI1NiJ9" + ".eyJzdWIiOiIxMjM0NTY3ODkwIn0" + ".dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U";

const catalog = {
  agents: [
    { name: "designer", mcpServers: ["linear", "github"] },
    { name: "backend", mcpServers: [] },
    { name: "writer", mcpServers: ["github"] },
  ],
  mcpServers: [
    { name: "github", transport: "stdio", command: "npx", args: ["-y", "gh-mcp"], env: { TOKEN: "${GITHUB_TOKEN}", MODE: "fast" }, url: "", headers: {}, agents: ["designer", "writer"], projects: [{ id: "p1", name: "harness" }] },
    { name: "linear", transport: "http", command: "", args: [], env: {}, url: "https://mcp.linear.app/mcp", headers: { Authorization: "Bearer ${LINEAR_API_KEY}" }, agents: ["designer"], projects: [] },
    { name: "keeper", transport: "http", command: "", args: [], env: {}, url: "https://x.test/mcp", headers: { Authorization: `Bearer ${LIN}` }, agents: [], projects: [] },
  ],
};
const projects = [{ id: "p1", name: "harness" }, { id: "p2", name: "billing-api" }];

test("a token is told from a plain word by its shape", () => {
  assert.equal(detectToken(GH)?.kind, "GitHub token");
  assert.equal(detectToken(SK)?.kind, "API key");
  assert.equal(detectToken(LIN)?.kind, "Linear API key");
  assert.equal(detectToken(JWT)?.kind, "JWT");
  assert.equal(detectToken("xoxb-" + "1234567890-abcdefghijklmnop")?.kind, "Slack token");
  assert.equal(detectToken("A".repeat(8) + "1234567890abcdefghijklmnopqrstuv")?.kind, "long secret");
  for (const plain of ["production", "true", "https://mcp.linear.app/mcp", "application/json", "my-sk-test", "sk-", "sk-short", "ghp_", "${LINEAR_API_KEY}", "Bearer ${LINEAR_API_KEY}", "", "/Users/me/projects/some-long-directory-name/with-parts"]) {
    assert.equal(detectToken(plain), null, `${JSON.stringify(plain)} is not a token`);
  }
});

test("a token behind an auth scheme is found, and only the token is the secret", () => {
  const found = detectToken(`Bearer ${LIN}`);
  assert.equal(found?.kind, "Linear API key");
  assert.equal(found?.secret, LIN);
  assert.equal(detectToken(`Basic ${GH}`)?.secret, GH);
  assert.equal(detectToken(`--key=${SK}`)?.secret, SK);
});

test("a pasted command line splits into the command and its arguments, quotes honoured", () => {
  assert.deepEqual(splitCommandLine("npx -y @linear/mcp-server"), ["npx", "-y", "@linear/mcp-server"]);
  assert.deepEqual(splitCommandLine(`uvx mcp-server-git --repository "/Users/me/my repo"`), ["uvx", "mcp-server-git", "--repository", "/Users/me/my repo"]);
  assert.deepEqual(splitCommandLine(`node server.js --name 'two words' --flag=""`), ["node", "server.js", "--name", "two words", "--flag="]);
  assert.deepEqual(splitCommandLine(`say "it's" fine`), ["say", "it's", "fine"]);
  assert.deepEqual(splitCommandLine(`a\\ b c`), ["a b", "c"]);
  assert.deepEqual(splitCommandLine("  npx   -y  "), ["npx", "-y"]);
  assert.deepEqual(splitCommandLine(`npx "unterminated arg`), ["npx", "unterminated arg"]);
  assert.deepEqual(splitCommandLine(`run --token \${TOKEN}`), ["run", "--token", "${TOKEN}"]);
  assert.deepEqual(splitCommandLine(""), []);
});

test("the variable a value moves to is named from the header, else from the server", () => {
  assert.equal(referenceName({ kind: "header", key: "Authorization", server: "linear" }), "LINEAR_API_KEY");
  assert.equal(referenceName({ kind: "header", key: "X-Api-Key", server: "my-server" }), "MY_SERVER_API_KEY");
  assert.equal(referenceName({ kind: "header", key: "X-Figma-Token", server: "figma" }), "FIGMA_TOKEN");
  assert.equal(referenceName({ kind: "header", key: "X-Workspace", server: "linear" }), "LINEAR_WORKSPACE_TOKEN");
  assert.equal(referenceName({ kind: "env", key: "LINEAR_API_KEY", server: "linear" }), "LINEAR_API_KEY");
  assert.equal(referenceName({ kind: "env", key: "token", server: "github" }), "TOKEN");
  assert.equal(referenceName({ kind: "env", key: "", server: "github" }), "GITHUB_API_KEY");
  assert.equal(referenceName({ kind: "arg", key: "", server: "github" }), "GITHUB_API_KEY");
  assert.equal(referenceName({ kind: "header", key: "Authorization", server: "" }), "MCP_API_KEY");
  assert.equal(referenceName({ kind: "header", key: "Authorization", server: "9lives" }), "MCP_9LIVES_API_KEY");
});

test("Use ${VAR} instead keeps the auth scheme and drops the secret", () => {
  assert.equal(useReference(`Bearer ${LIN}`, "LINEAR_API_KEY"), "Bearer ${LINEAR_API_KEY}");
  assert.equal(useReference(`Basic ${GH}`, "X"), "Basic ${X}");
  assert.equal(useReference(GH, "GITHUB_TOKEN"), "${GITHUB_TOKEN}");
  assert.equal(useReference(`--key=${SK}`, "K"), "--key=${K}");
  assert.equal(toReference("LINEAR_API_KEY"), "${LINEAR_API_KEY}");
  assert.equal(toReference("${LINEAR_API_KEY}"), "${LINEAR_API_KEY}");
  assert.equal(toReference("Bearer ${X}"), "Bearer ${X}");
  assert.equal(toReference("two words"), "two words");
});

test("${VAR} references are gathered from every field, once, in order", () => {
  const d = mcpDraft(catalog, { name: "x" });
  d.transport = "stdio";
  d.command = "run-${A}";
  d.args = [{ id: 1, value: "--k=${B}" }, { id: 2, value: "${A}" }];
  d.env = [{ id: 3, key: "T", value: "${C}", mode: "env", kept: false }];
  d.url = "https://${D}/x";
  d.headers = [{ id: 4, key: "H", value: "Bearer ${E}", mode: "env", kept: false }];
  assert.deepEqual(mcpRefs(d), ["A", "B", "C"], "only the fields of the chosen transport count");
  d.transport = "http";
  assert.deepEqual(mcpRefs(d), ["D", "E"]);
});

test("a new form is empty and unchanged; an edit starts from the server; a copy starts unnamed", () => {
  const fresh = mcpDraft(catalog, {});
  assert.equal(fresh.previousName, "");
  assert.equal(fresh.transport, "stdio");
  assert.equal(mcpChanges(fresh), 0);

  const edit = mcpDraft(catalog, { previous: catalog.mcpServers[0] });
  assert.equal(edit.previousName, "github");
  assert.equal(edit.name, "github");
  assert.equal(edit.command, "npx");
  assert.deepEqual(edit.args.map((a) => a.value), ["-y", "gh-mcp"]);
  assert.deepEqual(edit.env.map((r) => [r.key, r.value, r.mode]), [["TOKEN", "${GITHUB_TOKEN}", "env"], ["MODE", "fast", "text"]]);
  assert.deepEqual(edit.agents, ["designer", "writer"]);
  assert.deepEqual(edit.projects, ["p1"]);
  assert.equal(mcpChanges(edit), 0);

  const copy = mcpDraft(catalog, { from: catalog.mcpServers[1] });
  assert.equal(copy.previousName, "");
  assert.equal(copy.name, "");
  assert.equal(copy.url, "https://mcp.linear.app/mcp");
  assert.deepEqual(copy.agents, [], "a copy reaches nobody until it is picked");
});

test("changes count each field and each agent or project added or taken away", () => {
  const d = mcpDraft(catalog, { previous: catalog.mcpServers[0] });
  d.name = "github2";
  assert.equal(mcpChanges(d), 1);
  d.args.push({ id: 99, value: "--x" });
  assert.equal(mcpChanges(d), 2);
  d.agents = ["designer", "backend"];
  assert.equal(mcpChanges(d), 4, "one agent added, one taken away");
  d.projects = ["p1", "p2"];
  assert.equal(mcpChanges(d), 5);
  d.env[1].value = "slow";
  assert.equal(mcpChanges(d), 6);
});

test("a rename note counts the agents and projects that have the server", () => {
  const edit = mcpDraft(catalog, { previous: catalog.mcpServers[0] });
  assert.equal(rename(catalog, edit).show, true);
  assert.equal(rename(catalog, edit).text, "Renaming rewrites the name in 2 agents and 1 project. One save changes them all, or none.");
  assert.equal(rename(catalog, mcpDraft(catalog, {})).show, false);
  const lone = mcpDraft(catalog, { previous: catalog.mcpServers[1] });
  assert.equal(rename(catalog, lone).text, "Renaming rewrites the name in 1 agent and 0 projects. One save changes them all, or none.");
  const none = mcpDraft(catalog, { previous: catalog.mcpServers[2] });
  assert.equal(rename(catalog, none).text, "No agent and no project has this server, so a rename changes nothing else.");
});

test("the form checks the name, the transport's field and each row", () => {
  const d = mcpDraft(catalog, {});
  assert.equal(checkMcpDraft(d, catalog).valid, false);
  assert.equal(checkMcpDraft(d, catalog).nameProblem, "", "an untouched name does not scold");
  assert.equal(checkMcpDraft(d, catalog, { touched: true }).nameProblem, "Server name is required.");

  d.name = "bad name";
  assert.equal(checkMcpDraft(d, catalog).nameProblem, "Server name uses letters, digits, underscores and hyphens.");
  d.name = "github";
  const taken = checkMcpDraft(d, catalog);
  assert.equal(taken.nameProblem, "A server named github already exists.");
  assert.deepEqual(taken.taken, { name: "github", href: "/agents/mcp/github", suggestion: "github-2" });
  d.name = "Postman_2";
  assert.equal(checkMcpDraft(d, catalog).nameProblem, "");

  assert.equal(checkMcpDraft(d, catalog, { touched: true }).commandProblem, "Command is required for a local server.");
  d.command = "npx";
  assert.equal(checkMcpDraft(d, catalog).valid, true);
  d.transport = "http";
  assert.equal(checkMcpDraft(d, catalog, { touched: true }).urlProblem, "URL is required for a remote server.");
  d.url = "ftp://x";
  assert.equal(checkMcpDraft(d, catalog).urlProblem, "The URL starts with http:// or https://.");
  d.url = "https://mcp.example.com/mcp";
  assert.equal(checkMcpDraft(d, catalog).valid, true);
  d.url = "https://${HOST}/mcp";
  assert.equal(checkMcpDraft(d, catalog).valid, true, "a variable may stand in the URL");
});

test("renaming a server onto itself is not a clash, onto another is", () => {
  const d = mcpDraft(catalog, { previous: catalog.mcpServers[1] });
  assert.equal(checkMcpDraft(d, catalog).nameProblem, "");
  d.name = "github";
  assert.equal(checkMcpDraft(d, catalog).nameProblem, "A server named github already exists.");
});

test("a variable or header listed twice, or a value with no name, is refused before the server is asked", () => {
  const d = mcpDraft(catalog, {});
  d.name = "x";
  d.command = "run";
  d.env = [{ id: 1, key: "A", value: "1", mode: "text", kept: false }, { id: 2, key: "A", value: "2", mode: "text", kept: false }];
  assert.equal(checkMcpDraft(d, catalog).envProblem, "A is listed twice.");
  d.env = [{ id: 1, key: "", value: "1", mode: "text", kept: false }];
  assert.equal(checkMcpDraft(d, catalog).envProblem, "A variable with a value needs a name.");
  d.env = [{ id: 1, key: "BAD NAME", value: "1", mode: "text", kept: false }];
  assert.equal(checkMcpDraft(d, catalog).envProblem, "BAD NAME is not a variable name: use letters, digits and underscores, not starting with a digit.");
  d.env = [{ id: 1, key: "", value: "", mode: "env", kept: false }];
  assert.equal(checkMcpDraft(d, catalog).envProblem, "", "an empty row is left out, not an error");
  d.transport = "http";
  d.url = "https://x.test";
  d.headers = [{ id: 1, key: "Authorization", value: "a", mode: "text", kept: false }, { id: 2, key: "authorization", value: "b", mode: "text", kept: false }];
  assert.equal(checkMcpDraft(d, catalog).headersProblem, "authorization is listed twice.");
  d.headers = [{ id: 1, key: "Bad Header", value: "a", mode: "text", kept: false }];
  assert.equal(checkMcpDraft(d, catalog).headersProblem, "Bad Header is not a header name: use letters, digits and hyphens.");
});

test("a malformed ${…} in a value is named", () => {
  const d = mcpDraft(catalog, {});
  d.name = "x";
  d.command = "run";
  d.env = [{ id: 1, key: "A", value: "${1BAD}", mode: "env", kept: false }];
  assert.equal(checkMcpDraft(d, catalog).envProblem, "${1BAD} is not a variable name: use letters, digits and underscores, not starting with a digit.");
  d.env = [{ id: 1, key: "A", value: "${OPEN", mode: "env", kept: false }];
  assert.equal(checkMcpDraft(d, catalog).envProblem, "${OPEN is missing its closing brace.");
});

test("a token in a plain value turns its row into a warning and blocks the form until it is settled", () => {
  const d = mcpDraft(catalog, {});
  d.name = "linear";
  d.transport = "http";
  d.url = "https://mcp.linear.app/mcp";
  d.headers = [{ id: 7, key: "Authorization", value: `Bearer ${LIN}`, mode: "text", kept: false }];
  const check = checkMcpDraft(d, { mcpServers: [] });
  assert.equal(check.blocked, true);
  assert.equal(check.valid, false);
  assert.equal(check.secrets.length, 1);
  assert.deepEqual({ ...check.secrets[0], secret: undefined }, { section: "headers", id: 7, state: "warn", kind: "Linear API key", variable: "LINEAR_API_KEY", secret: undefined });
  assert.ok(!JSON.stringify(check).includes(LIN), "the warning never carries the value");

  d.headers[0].kept = true;
  const kept = checkMcpDraft(d, { mcpServers: [] });
  assert.equal(kept.blocked, false);
  assert.equal(kept.valid, true);
  assert.equal(kept.secrets[0].state, "kept");
  assert.ok(!JSON.stringify(kept).includes(LIN));
});

test("a token among the arguments warns too, and a variable never does", () => {
  const d = mcpDraft(catalog, {});
  d.name = "gh";
  d.command = "npx";
  d.args = [{ id: 1, value: "--token" }, { id: 2, value: GH }, { id: 3, value: "${OTHER}" }];
  const check = checkMcpDraft(d, catalog);
  assert.deepEqual(check.secrets.map((s) => [s.section, s.id, s.state]), [["args", 2, "warn"]]);
  d.env = [{ id: 5, key: "GITHUB_TOKEN", value: "${GITHUB_TOKEN}", mode: "env", kept: false }];
  assert.equal(checkMcpDraft(d, catalog).secrets.length, 1);
});

test("a stored plain-text token opens as kept: the choice was made when it was saved", () => {
  const d = mcpDraft(catalog, { previous: catalog.mcpServers[2] });
  assert.equal(d.headers[0].mode, "text");
  assert.equal(d.headers[0].kept, true);
  const check = checkMcpDraft(d, catalog);
  assert.equal(check.blocked, false);
  assert.equal(check.secrets[0].state, "kept");
});

test("switching a row to From env or Plain text sets its mode, and a token or an empty value becomes a reference", () => {
  const d = mcpDraft(catalog, {});
  d.name = "linear";
  d.headers = [{ id: 1, key: "Authorization", value: `Bearer ${LIN}`, mode: "text", kept: false }, { id: 2, key: "X-Trace", value: "abc", mode: "text", kept: false }, { id: 3, key: "X-Api-Key", value: "", mode: "text", kept: false }];
  setMode(d, "headers", 1, "env");
  assert.deepEqual([d.headers[0].mode, d.headers[0].value], ["env", "Bearer ${LINEAR_API_KEY}"]);
  setMode(d, "headers", 2, "env");
  assert.deepEqual([d.headers[1].mode, d.headers[1].value], ["env", "abc"], "an ordinary value is left for the person");
  setMode(d, "headers", 3, "env");
  assert.equal(d.headers[2].value, "${LINEAR_API_KEY}");
  setMode(d, "headers", 3, "text");
  assert.deepEqual([d.headers[2].mode, d.headers[2].value], ["text", "${LINEAR_API_KEY}"], "going back to text keeps what is there");
});

test("rows are added with fresh ids, From env by default", () => {
  const d = mcpDraft(catalog, {});
  addRow(d, "args");
  addRow(d, "env");
  addRow(d, "env");
  addRow(d, "headers");
  assert.equal(d.args.length, 1);
  assert.deepEqual(d.env.map((r) => r.mode), ["env", "env"]);
  assert.equal(new Set([d.args[0].id, d.env[0].id, d.env[1].id, d.headers[0].id]).size, 4);
  removeRow(d, "env", d.env[0].id);
  assert.equal(d.env.length, 1);
  removeRow(d, "args", d.args[0].id);
  assert.deepEqual(d.args, []);
});

test("the save sends the chosen transport's fields, skips empty rows and trims", () => {
  const d = mcpDraft(catalog, { previous: catalog.mcpServers[0] });
  d.name = " github2 ";
  d.command = " npx ";
  d.args = [{ id: 1, value: "-y" }, { id: 2, value: "" }, { id: 3, value: "gh-mcp" }];
  d.env = [{ id: 4, key: " TOKEN ", value: "${GITHUB_TOKEN}", mode: "env", kept: false }, { id: 5, key: "", value: "", mode: "env", kept: false }];
  d.url = "https://left.over";
  d.headers = [{ id: 6, key: "A", value: "b", mode: "text", kept: false }];
  d.agents = ["backend"];
  d.projects = ["p2"];
  assert.deepEqual(mcpSave(d), {
    previousName: "github",
    server: { name: "github2", transport: "stdio", command: "npx", args: ["-y", "gh-mcp"], env: { TOKEN: "${GITHUB_TOKEN}" }, url: "", headers: {} },
    agents: ["backend"],
    projects: ["p2"],
  });
  d.transport = "http";
  d.url = " https://mcp.example.com ";
  assert.deepEqual(mcpSave(d).server, { name: "github2", transport: "http", command: "", args: [], env: {}, url: "https://mcp.example.com", headers: { A: "b" } });
  assert.equal(mcpSave(mcpDraft(catalog, {})).previousName, undefined);
});

test("an SSE server stays SSE under the Remote URL card", () => {
  const d = mcpDraft(catalog, { previous: { ...catalog.mcpServers[1], transport: "sse" } });
  assert.equal(d.transport, "sse");
  assert.equal(mcpSave(d).server.transport, "sse");
});

test("the drawer's save leaves the server unattached; the form's save sends the lists as picked", () => {
  const d = mcpDraft(catalog, {});
  d.name = "x";
  d.command = "run";
  d.agents = ["designer"];
  assert.deepEqual(mcpSave(d, { unattached: true }).agents, []);
  assert.deepEqual(mcpSave(d, { unattached: true }).projects, []);
  assert.deepEqual(mcpSave(d).agents, ["designer"]);
});

test("a kept secret is hidden in what the page shows of the server's answer", () => {
  const d = mcpDraft(catalog, {});
  d.name = "linear";
  d.transport = "http";
  d.url = "https://mcp.linear.app/mcp";
  d.headers = [{ id: 1, key: "Authorization", value: `Bearer ${LIN}`, mode: "text", kept: true }];
  const secrets = checkMcpDraft(d, catalog).secrets;
  const answer = {
    summary: "Created mcp-servers.json",
    writes: [{ path: "mcp-servers.json", before: '{\n  "mcpServers": {}\n}\n', after: `{\n  "mcpServers": {\n    "linear": { "headers": { "Authorization": "Bearer ${LIN}" } }\n  }\n}\n` }],
    projects: [],
  };
  const shown = hideSecrets(answer, d);
  assert.ok(!JSON.stringify(shown).includes(LIN));
  assert.ok(shown.writes[0].after.includes("Bearer ••••••"));
  assert.equal(shown.writes[0].before, answer.writes[0].before);
  assert.ok(answer.writes[0].after.includes(LIN), "the answer itself is not changed");
  assert.equal(secrets.length, 1);
});

test("the preview lays out each file of the dry run and each project list it sets", () => {
  const writes = [
    { path: "mcp-servers.json", before: null, after: '{ "mcpServers": {} }\n' },
    { path: "agents.json", before: '{\n  "a": 1\n}\n', after: '{\n  "a": 2\n}\n' },
  ];
  const files = mcpPreviewModel({ writes, projects: [{ id: "p1", name: "harness", before: ["x"], after: ["x", "linear"] }] });
  assert.deepEqual(files.map((f) => [f.path, f.badge]), [["mcp-servers.json", "new"], ["agents.json", "edit"], ["project “harness”", "saved with the project"]]);
  assert.equal(files[0].isText, true);
  assert.equal(files[1].isDiff, true);
  assert.equal(files[2].isText, true);
  assert.equal(files[2].text, 'mcp_servers: [ "x", "linear" ]');
  assert.equal(mcpPreviewModel({ writes: [], projects: [] }).length, 0);
});

test("who gets it says how many agents and projects, or that the server is unused", () => {
  const d = mcpDraft(catalog, {});
  assert.deepEqual(whoGetsIt(d), { agents: 0, projects: 0, unused: true, text: "Unused. No agent and no project gets this server yet. That is allowed.", short: "Unused: no agent and no project gets it yet." });
  d.agents = ["a", "b"];
  d.projects = ["p"];
  const w = whoGetsIt(d);
  assert.equal(w.unused, false);
  assert.equal(w.text, "Reaches 2 agents and 1 project, from their next session. Running sessions keep what they started with.");
  assert.equal(w.short, "Reaches 2 agents and 1 project from their next session.");
  d.agents = ["a"];
  d.projects = [];
  assert.equal(whoGetsIt(d).short, "Reaches 1 agent and 0 projects from their next session.");
});

test("a refusal lands on its field: a taken name, a bad row, the rest on the form", () => {
  const refuse = (code, message, details) => describeRefusal(new StructuredError(code, message, 400, details));
  assert.deepEqual(mcpFieldErrors(null), { name: "", command: "", url: "", env: "", headers: "", args: "", form: "", suggestion: "" });
  const taken = mcpFieldErrors(refuse("NAME_TAKEN", "exists", { field: "name", suggestion: "linear-2" }));
  assert.equal(taken.name, "That name is taken. Try linear-2.");
  assert.equal(taken.suggestion, "linear-2");
  assert.equal(mcpFieldErrors(refuse("INVALID_INPUT", "a stdio server needs a command", { field: "command" })).command, "a stdio server needs a command");
  assert.equal(mcpFieldErrors(refuse("INVALID_INPUT", "needs a url", { field: "server.url" })).url, "needs a url");
  assert.equal(mcpFieldErrors(refuse("INVALID_INPUT", "a header needs a name", { field: "headers" })).headers, "a header needs a name");
  assert.equal(mcpFieldErrors(refuse("INVALID_INPUT", "bad", { field: "name" })).name, "bad");
  assert.equal(mcpFieldErrors(refuse("INVALID_INPUT", "odd", { field: "somewhere" })).form, "odd");
  assert.equal(mcpFieldErrors(refuse("UNKNOWN_REFERENCE", "x", { names: ["p9"] })).form, "These do not exist: p9.");
  assert.equal(mcpFieldErrors(refuse("CATALOG_BROKEN", "mcp-servers.json is broken")).form, "mcp-servers.json is broken");
});

test("the probe's answer reads as success with its tool names, or a failure with its cause and hint", () => {
  assert.deepEqual(probeView(null), { phase: "idle", tone: "", title: "", detail: "", tools: [], more: 0, hint: "", canSave: "" });
  const ok = probeView({ status: "ok", tools: Array.from({ length: 9 }, (_, i) => ({ name: `tool_${i}`, description: "" })), error: "", hint: "" });
  assert.equal(ok.phase, "ok");
  assert.equal(ok.tone, "success");
  assert.equal(ok.title, "Connected. 9 tools:");
  assert.deepEqual(ok.tools, ["tool_0", "tool_1", "tool_2", "tool_3", "tool_4", "tool_5"]);
  assert.equal(ok.more, 3);
  assert.equal(probeView({ status: "ok", tools: [{ name: "a", description: "" }], error: "", hint: "" }).title, "Connected. 1 tool:");
  assert.equal(probeView({ status: "ok", tools: [], error: "", hint: "" }).title, "Connected. The server lists no tools.");
  const bad = probeView({ status: "error", tools: [], error: "401 Unauthorized", hint: "LINEAR_API_KEY is not set in the harness server's environment." });
  assert.equal(bad.phase, "error");
  assert.equal(bad.tone, "danger");
  assert.equal(bad.title, "401 Unauthorized");
  assert.equal(bad.hint, "LINEAR_API_KEY is not set in the harness server's environment.");
  assert.equal(bad.canSave, "You can still save; agents will see the server fail to start.");
  assert.equal(probeView({ status: "error", tools: [], error: "", hint: "" }).title, "The server did not connect.");
});
