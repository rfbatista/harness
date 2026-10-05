// Projects and repositories for tests.

export function makeProject(overrides = {}) {
  return Object.freeze({ id: "p1", name: "coding_pool", rootDir: "/src/coding_pool", ...overrides });
}

export function makeRepository(overrides = {}) {
  return Object.freeze({
    id: "r1",
    projectId: "p1",
    name: "harness",
    description: "",
    url: "git@github.com:me/harness.git",
    rootDir: "/src/coding_pool/harness",
    ...overrides,
  });
}

/** The wire format of a repository. */
export function repositoryDTO(r) {
  return { id: r.id, project_id: r.projectId, name: r.name, description: r.description || undefined, url: r.url, root_dir: r.rootDir };
}
