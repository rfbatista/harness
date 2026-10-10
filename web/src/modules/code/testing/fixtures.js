// A small world for the code views: one repository at main and a tag, a
// session's worktree with committed and uncommitted work, two commits, and
// an architecture with contexts, a loose zone and unzoned files. Between
// them they produce every state of UX §5 the tests need.

const hash = (c) => c.repeat(40);
export const MAIN = hash("a");
export const TAG = hash("b");
export const HEAD = hash("c");
export const C1 = hash("1");
export const C2 = hash("2");

const go = (name, body = "") => `package ${name}\n\nimport "context"\n\n// ${name} does one thing.\nfunc Run(ctx context.Context) error {\n${body}\treturn nil\n}\n`;
const many = (n, word = "line") => Array.from({ length: n }, (_, i) => `${word} ${i + 1}`).join("\n") + "\n";

const mainFiles = () => ({
  "README.md": "# harness\n\nRead code in the web UI.\n",
  ".gitkeep": "",
  "internal/feed/feed.go": go("feed"),
  "internal/ports/workspace.go": go("ports"),
  "web/app.js": 'export const app = () => "hi";\n',
  "docs/old.md": many(60, "old"),
  "assets/logo.png": { binary: 49_152 },
  "third_party/xterm": { submodule: true },
  "web/link.js": { symlink: "app.js" },
  "web/out": { symlink: "../../etc/passwd" },
});

/** @returns {import("../infrastructure/memory-gateway.js").World} */
export function makeWorld(overrides = {}) {
  const main = mainFiles();
  const committed = { ...main, "internal/feed/feed.go": go("feed", "\tctx.Done()\n"), "internal/code/browser.go": go("code") };
  delete committed["docs/old.md"];
  const disk = { ...committed, "README.md": "# harness\n\nRead and follow code in the web UI.\n", "notes/todo.md": "- [ ] ship it\n" };
  return {
    repositories: {
      r1: {
        name: "harness",
        head: "main",
        refs: {
          main: { commit: MAIN, files: main },
          "v0.9.0": { commit: TAG, files: { "README.md": "# harness\n" } },
          "agent/visualize-code": { commit: HEAD, files: committed },
        },
      },
      empty: { name: "empty", head: "main", refs: {} },
    },
    workspaces: {
      ws1: { repositoryId: "r1", branch: "agent/visualize-code", base: "main", head: HEAD, committed, disk, untracked: ["notes/todo.md"] },
    },
    commits: {
      [C1]: { repositoryId: "r1", parent: "", files: { "README.md": "# harness\n" } },
      [C2]: { repositoryId: "r1", parent: C1, files: { "README.md": "# harness\n\nRead code.\n", "web/app.js": 'export const app = () => "hi";\n' } },
    },
    architecture: {
      contexts: [
        { id: "ctx-ws", name: "Workspaces", purpose: "Repositories, worktrees and git reads." },
        { id: "ctx-web", name: "Web", purpose: "The browser half." },
      ],
      zones: [
        { id: "z-feed", name: "feed", pattern: "internal/feed/**", contextId: "ctx-ws" },
        { id: "z-ports", name: "ports", pattern: "internal/ports/**", contextId: "ctx-ws" },
        { id: "z-web", name: "web", pattern: "web/**", contextId: "ctx-web" },
        { id: "z-docs", name: "docs", pattern: "docs/**", contextId: "" },
      ],
    },
    ...overrides,
  };
}

/** A worktree file of `n` lines, for virtualisation and the large-diff states. */
export const longFile = (n) => many(n, "row");
