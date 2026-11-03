// A world for the RepositoryAgentsGateway contract: atlas in Selected mode,
// enabling reviewer, tester and backend, with backend unassigned and docs
// (not enabled) unassigned too.

/** @param {object} [over] */
export function makeWorld(over = {}) {
  return {
    catalog: [
      { name: "reviewer", description: "Reviews code.", prompt: "You review code." },
      { name: "tester", description: "Writes tests.", prompt: "You write tests." },
      { name: "backend", description: "Builds the server.", prompt: "You build the server." },
      { name: "docs", description: "Writes docs.", prompt: "You write docs." },
      { name: "ops", description: "Runs things.", prompt: "You run things." },
    ],
    projects: [{ id: "p1", name: "atlas", agentsMode: "selected", enabled: ["reviewer", "tester", "backend"] }],
    repositories: [
      { id: "r1", name: "atlas-api", projectId: "p1" },
      { id: "r2", name: "atlas-web", projectId: "p1" },
    ],
    unassigned: { r1: ["backend", "docs"] },
    prompts: {
      "r1/reviewer": "Run make check before every commit.\nThen open the PR.",
      "r1/backend": "Mind the migrations.",
    },
    running: { "r1/reviewer": 1 },
    renamed: { reviewer: "rev" },
    ...over,
  };
}
