// A small catalog for tests, in the wire format the server speaks (contract
// section 3); the memory gateway and the stub API start from it.

export function catalogDTO() {
  return {
    root: "/data/agents",
    versions: { "agents.json": "sha256:a1", "mcp-servers.json": "sha256:m1" },
    broken: null,
    agents: [
      {
        name: "ux-designer", description: "Designs screens.", aliases: ["ux"],
        entries: ["@ux", "tdd"], resolved_skills: ["lint", "tdd"], dropped_duplicates: [], missing_skills: [],
        mcp_servers: ["github"], allowed_tools: ["Read"], usage: { sessions: 3, zones: 1 }, problems: [],
      },
      {
        name: "backend", description: "", aliases: [],
        entries: ["tdd", "ghost"], resolved_skills: ["tdd"], dropped_duplicates: [], missing_skills: ["ghost"],
        mcp_servers: [], allowed_tools: [],
        problems: [{ code: "NO_DESCRIPTION", message: "no description" }, { code: "MISSING_SKILL", message: "skill ghost does not exist" }],
      },
    ],
    bundles: [{ name: "ux", entries: ["lint"], resolved_skills: ["lint"], used_by_agents: ["ux-designer"], used_by_bundles: [] }],
    skills: [
      { name: "tdd", description: "Test first.", file_count: 3, has_extra_files: true, agents: ["ux-designer", "backend"], bundles: [], agent_count: 2, problems: [] },
      { name: "lint", description: "Lint it.", file_count: 1, has_extra_files: false, agents: [], bundles: ["ux"], agent_count: 1, problems: [] },
    ],
    mcp_servers: [
      {
        name: "github", transport: "stdio", command: "npx", args: ["-y", "gh-mcp"], env: { TOKEN: "${GITHUB_TOKEN}" }, url: "", headers: {},
        env_refs: [{ name: "GITHUB_TOKEN", set: false }], agents: ["ux-designer"], projects: [{ id: "p1", name: "harness" }],
        problems: [{ code: "SECRET_NOT_SET", message: "GITHUB_TOKEN is not set" }],
      },
    ],
    built_in_tools: [{ name: "Read", description: "Reads a file." }, { name: "Bash", description: "Runs a command." }],
  };
}

/**
 * Skills that ship with the server, in the wire format (contract revision 2, ADR 0088):
 * `task-run-app` for the architect, `task-tdd` for the implement mode. Opt-in, so a test
 * that does not care about them keeps the small catalog above.
 * @param {ReturnType<typeof catalogDTO>} dto
 */
export function withBuiltInSkills(dto) {
  dto.skills.push(
    { name: "task-run-app", description: "Runs the task's application.", file_count: 1, has_extra_files: false, agents: [], bundles: [], agent_count: 0, problems: [], built_in: true, modes: ["architect"] },
    { name: "task-tdd", description: "Builds a ticket test first.", file_count: 1, has_extra_files: false, agents: [], bundles: [], agent_count: 0, problems: [], built_in: true, modes: ["implement", "architect"] },
  );
  return dto;
}

/** The world for the memory gateway. */
export function makeWorld() {
  return { catalog: catalogDTO(), projects: { p1: ["github"] }, projectNames: { p1: "harness" }, env: { GITHUB_TOKEN: false, HOME: true } };
}
