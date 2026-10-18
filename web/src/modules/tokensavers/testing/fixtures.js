// Token savers for tests: a world for the memory gateway, and the wire
// format the server speaks, for the stub API.

export const BUILT = new Date("2026-10-18T09:00:00Z");

/** @returns {import("../domain/installation.js").Saver} */
export function makeSaver(overrides = {}) {
  return {
    saver: "rtk",
    summary: "Compresses shell output before the agent reads it.",
    pinnedVersion: "v0.30.1",
    state: "installed",
    installedVersion: "v0.30.1",
    path: "/data/tools/rtk/v0.30.1/rtk",
    progress: "",
    error: null,
    onSomewhere: false,
    ...overrides,
  };
}

/** @returns {import("../domain/repository-index.js").RepositoryIndex} */
export function makeIndex(overrides = {}) {
  return {
    repositoryId: "r1",
    repositoryName: "api",
    projectName: "harness",
    checkout: "/src/api",
    builtAt: BUILT,
    building: false,
    error: "",
    ...overrides,
  };
}

/** The world the contract runs against. */
export function makeWorld() {
  return {
    activations: [
      {
        level: "project",
        ownerId: "p1",
        activations: ["rtk", "caveman", "codegraph"].map((saver) => Object.freeze({ saver, value: "off", summary: "off (project)" })),
      },
    ],
    savers: {
      savers: [
        makeSaver(),
        makeSaver({ saver: "caveman", pinnedVersion: "v4.0.1", state: "not_installed", installedVersion: "", path: "", onSomewhere: true }),
        makeSaver({ saver: "codegraph", pinnedVersion: "v1.6.2", installedVersion: "v1.6.2", path: "/data/tools/codegraph/v1.6.2/bin/codegraph" }),
      ],
      platform: "darwin/arm64",
      supported: true,
      unsupported: "",
    },
    indexes: [makeIndex(), makeIndex({ repositoryId: "r2", repositoryName: "docs", checkout: "", builtAt: null })],
    intensity: "full",
  };
}

export function activationsDTO(a) {
  return { level: a.level, owner_id: a.ownerId, activations: a.activations.map((x) => ({ saver: x.saver, value: x.value, summary: x.summary })) };
}

export function saverDTO(s) {
  return {
    saver: s.saver,
    summary: s.summary,
    pinned_version: s.pinnedVersion,
    state: s.state,
    installed: s.installedVersion ? { version: s.installedVersion, path: s.path } : undefined,
    progress: s.progress || undefined,
    error: s.error ?? undefined,
    on_somewhere: s.onSomewhere,
  };
}

export function saverListDTO(l) {
  return { token_savers: l.savers.map(saverDTO), platform: l.platform, supported: l.supported, unsupported: l.unsupported || undefined };
}

export function indexDTO(i) {
  return {
    repository_id: i.repositoryId,
    repository_name: i.repositoryName,
    project_name: i.projectName,
    checkout: i.checkout,
    built_at: i.builtAt ? i.builtAt.toISOString() : undefined,
    building: i.building,
    error: i.error || undefined,
  };
}
