// TokenSaverGateway in memory: the fake for tests. It obeys the same
// contract as the real gateway (../testing/gateway-contract.js): the
// server's refusals, and installs and builds that run until the test
// finishes them.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { toActivations, toRepositoryIndexes, toSaverList } from "./dto.js";

const fail = (code, message, status) => {
  throw new StructuredError(code, `${code}: ${message}`, status);
};

const OWNER_NOT_FOUND = { project: Codes.PROJECT_NOT_FOUND, repository: Codes.REPOSITORY_NOT_FOUND, task: Codes.TICKET_NOT_FOUND };
const INTENSITIES = ["lite", "full", "ultra"];

/**
 * @param {{
 *   activations?: import("../domain/ports.js").Activations[],  one per owner
 *   savers?: import("../domain/installation.js").SaverList,
 *   indexes?: import("../domain/repository-index.js").RepositoryIndex[],
 *   intensity?: import("../domain/ports.js").CavemanIntensity,
 * }} [world]
 */
export function memoryTokenSavers({ activations = [], savers = { savers: [], platform: "darwin/arm64", supported: true, unsupported: "" }, indexes = [], intensity = "full" } = {}) {
  const owners = new Map(activations.map((a) => [`${a.level}:${a.ownerId}`, a]));
  let list = { ...savers, savers: [...savers.savers] };
  let idx = [...indexes];
  let current = intensity;

  const mustFindSaver = (name) => list.savers.find((s) => s.saver === name) ?? fail("TOKEN_SAVER_NOT_FOUND", `there is no token saver "${name}"`, 404);
  const putSaver = (s) => {
    list = { ...list, savers: list.savers.map((x) => (x.saver === s.saver ? s : x)) };
    return s;
  };
  const putIndex = (i) => {
    idx = idx.map((x) => (x.repositoryId === i.repositoryId ? i : x));
    return i;
  };

  /** @type {import("../domain/ports.js").TokenSaverGateway} */
  const gateway = {
    decodeActivations: toActivations,
    async setActivation(level, ownerId, saver, value) {
      const owner = owners.get(`${level}:${ownerId}`) ?? fail(OWNER_NOT_FOUND[level] ?? Codes.INVALID_INPUT, `${level} ${ownerId} not found`, 404);
      const allowed = level === "project" ? ["on", "off"] : ["inherit", "on", "off"];
      if (!allowed.includes(value)) fail(Codes.INVALID_INPUT, `${value} is not a value at ${level} level`, 400);
      const row = owner.activations.find((a) => a.saver === saver) ?? fail(Codes.INVALID_INPUT, `there is no token saver "${saver}"`, 400);
      const next = { ...owner, activations: owner.activations.map((a) => (a === row ? Object.freeze({ ...a, value, summary: `${value} (${level})` }) : a)) };
      owners.set(`${level}:${ownerId}`, next);
      return next;
    },

    decodeSavers: toSaverList,
    async listSavers() {
      return list;
    },
    async installSaver(name) {
      const s = mustFindSaver(name);
      if (!list.supported) fail("TOKEN_SAVER_UNSUPPORTED_PLATFORM", list.unsupported, 400);
      if (s.state === "installing") return s;
      return putSaver({ ...s, state: "installing", progress: "Starting", error: null });
    },
    async removeSaver(name) {
      const s = mustFindSaver(name);
      if (s.state === "installing") fail("TOKEN_SAVER_BUSY", `${name} is installing; remove it once the install ends`, 409);
      return putSaver({ ...s, state: "not_installed", installedVersion: "", path: "", progress: "", error: null });
    },

    decodeIndexes: toRepositoryIndexes,
    async listIndexes() {
      return idx;
    },
    async rebuildIndex(repositoryId) {
      const i = idx.find((x) => x.repositoryId === repositoryId) ?? fail(Codes.REPOSITORY_NOT_FOUND, "repository not found", 404);
      if (!i.checkout) fail("INVALID_ROOT", "repository has no root_dir: there is no checkout to index", 400);
      if (!list.savers.some((s) => s.saver === "codegraph" && s.installedVersion)) fail("TOKEN_SAVER_NOT_INSTALLED", "codegraph is not installed", 400);
      return putIndex({ ...i, building: true, error: "" });
    },

    async setCavemanIntensity(value) {
      if (!INTENSITIES.includes(value)) fail(Codes.INVALID_INPUT, `${value} is not a caveman intensity`, 400);
      current = value;
      return current;
    },
  };

  return {
    gateway,
    /** The running install of saver ends at its pinned version. */
    finishInstall(name) {
      const s = mustFindSaver(name);
      putSaver({ ...s, state: "installed", installedVersion: s.pinnedVersion, progress: "" });
    },
    /** The running build of repositoryId ends, at `at`. */
    finishBuild(repositoryId, at) {
      const i = idx.find((x) => x.repositoryId === repositoryId);
      if (i) putIndex({ ...i, building: false, builtAt: at });
    },
    intensity: () => current,
  };
}
