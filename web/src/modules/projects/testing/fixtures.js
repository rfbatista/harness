// Projects and repositories for tests.

/** The home of the user the server runs as, which ~ expands to. */
export const HOME = "/home/you";
/** Directories that exist on the server's machine. */
export const DIRS = ["/src", "/src/harness", "/src/coding_pool", "/a", `${HOME}/work`];

export function makeProject(overrides = {}) {
  return Object.freeze({ id: "p1", name: "coding_pool", rootDir: "/src/coding_pool", ignoredPaths: [], ...overrides });
}

/** The wire format of a project. */
export function projectDTO(p) {
  return { id: p.id, name: p.name, root_dir: p.rootDir, ignored_paths: p.ignoredPaths?.length ? [...p.ignoredPaths] : undefined };
}

/** The wire format of a project summary. */
export function summaryDTO(s) {
  return {
    project: projectDTO(s.project),
    repository_count: s.repositoryCount,
    open_task_count: s.openTaskCount,
    running_session_count: s.runningSessionCount,
    last_activity_at: s.lastActivityAt ? s.lastActivityAt.toISOString() : null,
  };
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
