// The design system module's gateway over /api and the project feed: the
// endpoints of "Contract: Harness server ↔ Web UI — project design system".
// The wire shapes are read here and nowhere else.

import { StructuredError, Codes } from "../../../shared/domain/errors.js";
import { optInRow } from "../domain/repositories.js";

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @param {import("../../../shared/infrastructure/feed.js").Feed} feed
 */
export function designSystemGateway(api, feed) {
  const list = (xs) => (Array.isArray(xs) ? xs : []);

  /** The state of a get_design_system answer, or of the page's seed (the same shape). */
  const toState = (body) => {
    if (!body || typeof body.record !== "object" || body.record === null) {
      throw new StructuredError(Codes.BAD_RESPONSE, "The design system came back unreadable.");
    }
    return {
      exists: body.exists === true,
      version: Number.isInteger(body.version) ? body.version : 0,
      updatedAt: body.updated_at ?? "",
      updatedBy: body.updated_by ?? "",
      record: {
        tokens: list(body.record.tokens),
        principles: typeof body.record.principles === "string" ? body.record.principles : "",
        components: list(body.record.components),
      },
    };
  };

  // The endpoints are keyed by design system, the page by project: until the
  // page names a design system (ticket 13 re-keys this), it works on the
  // project's first one, made by the first save.
  const ids = new Map();
  const firstOf = async (projectId) => {
    if (ids.has(projectId)) return ids.get(projectId);
    const body = await api.post("/list_design_systems", { project_id: projectId });
    let first = null;
    for (const d of list(body?.design_systems)) if (!first || String(d.created_at) < String(first.created_at)) first = d;
    if (first) ids.set(projectId, first.design_system_id);
    return first?.design_system_id ?? "";
  };
  const ensure = async (projectId) => {
    const id = await firstOf(projectId);
    if (id) return id;
    const body = await api.post("/create_design_system", { project_id: projectId, name: "Design system", slug: "design-system" });
    ids.set(projectId, body.design_system_id);
    return body.design_system_id;
  };

  return {
    /** The state the server embedded in the page. */
    fromSeed: toState,

    async get(projectId) {
      const id = await firstOf(projectId);
      return toState(id ? await api.post("/get_design_system", { design_system_id: id }) : { record: {} });
    },

    /** Saves the record as the next version; DESIGN_SYSTEM_CHANGED and DESIGN_SYSTEM_INVALID arrive as errors. Answers the new version. */
    async save(projectId, baseVersion, record, message) {
      const body = await api.post("/save_design_system", { design_system_id: await ensure(projectId), base_version: baseVersion, record, message });
      return body.version;
    },

    /** Renames a saved token as its own version; answers the new version. */
    async rename(projectId, baseVersion, from, to) {
      const body = await api.post("/rename_design_token", {
        design_system_id: await ensure(projectId),
        base_version: baseVersion,
        group: from.group,
        name: from.name,
        new_group: to.group,
        new_name: to.name,
      });
      return body.version;
    },

    /** One page of the version list, newest first; `before` is a version number, the page holds those older than it. */
    async versions(projectId, { limit, before } = {}) {
      const id = await firstOf(projectId);
      const body = id ? await api.post("/list_design_system_versions", { design_system_id: id, limit, before }) : {};
      return list(body?.versions).map((v) => ({
        version: v.version,
        at: v.at ?? "",
        by: v.by ?? "",
        source: v.source ?? "",
        proposalId: v.proposal_id ?? "",
        message: v.message ?? "",
        summary: v.summary ?? {},
      }));
    },

    /** One version with its record, read only; VERSION_NOT_FOUND arrives as an error. */
    async version(projectId, version) {
      const id = await firstOf(projectId);
      if (!id) throw new StructuredError("VERSION_NOT_FOUND", "The design system has no version " + version + ".");
      const body = await api.post("/get_design_system_version", { design_system_id: id, version });
      const record = body?.record;
      if (!Number.isInteger(body?.version) || typeof record !== "object" || record === null) {
        throw new StructuredError(Codes.BAD_RESPONSE, "The version came back unreadable.");
      }
      return {
        version: body.version,
        at: body.at ?? "",
        record: { tokens: list(record.tokens), principles: typeof record.principles === "string" ? record.principles : "", components: list(record.components) },
      };
    },

    /**
     * The project's repositories with the design system each uses, as rows a page edits.
     * Until the page names a design system (ticket 15 re-keys this) it is asked about the project's first one.
     */
    async repositories(projectId) {
      const id = await firstOf(projectId);
      if (!id) return [];
      const body = await api.post("/list_design_system_repositories", { design_system_id: id });
      return list(body?.repositories).map(optInRow);
    },

    /** The project's design systems, by name, as the select lists them. */
    async designSystems(projectId) {
      const body = await api.post("/list_design_systems", { project_id: projectId });
      return list(body?.design_systems).map((d) => ({ id: d.design_system_id, name: d.name, version: Number.isInteger(d.version) ? d.version : 0 }));
    },

    /**
     * Sets the design system a repository uses (null: none) and where its export goes; an empty path leaves the server's own.
     * MOVE_NOT_CONFIRMED (details.current names the one in use) arrives as an error unless confirmMove; so do an invalid path
     * (INVALID_INPUT), an unknown repository and an unknown design system. Answers what is stored.
     */
    async apply(repositoryId, designSystemId, path, confirmMove = false) {
      const body = await api.post("/set_repository_design_system", {
        repository_id: repositoryId,
        design_system_id: designSystemId,
        ...(path ? { path } : {}),
        ...(confirmMove ? { confirm_move: true } : {}),
      });
      const repo = body?.repository;
      if (typeof repo?.id !== "string" || repo.id === "") {
        throw new StructuredError(Codes.BAD_RESPONSE, "The repository came back unreadable.");
      }
      return { id: repo.id, designSystemId: repo.design_system_id ?? "", path: repo.design_system_path ?? "" };
    },

    /** Applies the project's first design system (ticket 15 re-keys this) or removes the one in use. Answers what is stored. */
    async setRepository(repositoryId, uses, path, projectId, confirmMove = false) {
      const id = uses ? await firstOf(projectId) : "";
      if (uses && !id) throw new StructuredError(Codes.BAD_RESPONSE, "The project has no design system.");
      const stored = await this.apply(repositoryId, uses ? id : null, path, confirmMove);
      return { ...stored, uses: stored.designSystemId !== "" };
    },

    /** Follows the project feed for saves of the design system. */
    follow(projectId, { onSaved, onStatus }) {
      return feed.follow(`/events?project_id=${encodeURIComponent(projectId)}`, {
        onMessage(dto) {
          const saved = dto?.design_system_saved;
          if (saved && saved.project_id === projectId) onSaved?.({ version: Number.isInteger(saved.version) ? saved.version : 0, source: saved.source ?? "" });
        },
        onStatus,
      });
    },
  };
}
