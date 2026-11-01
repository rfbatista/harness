// The design system module's gateway over /api and the project feed: the
// endpoints of "Contract: Harness server ↔ Web UI — project design system".
// The wire shapes are read here and nowhere else.

import { StructuredError, Codes } from "../../../shared/domain/errors.js";
import { listRow } from "../domain/identity.js";
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

  // BRIDGE (ticket 15): the Repositories tab's rows and the repository
  // settings select still ask about the project's first design system when
  // they are not given one.
  const firstOf = async (projectId) => {
    const body = await api.post("/list_design_systems", { project_id: projectId });
    let first = null;
    for (const d of list(body?.design_systems)) if (!first || String(d.created_at) < String(first.created_at)) first = d;
    return first?.design_system_id ?? "";
  };

  return {
    /** The state the server embedded in the page. */
    fromSeed: toState,

    async get(designSystemId) {
      return toState(await api.post("/get_design_system", { design_system_id: designSystemId }));
    },

    /** A design system's name and description now; null once it is gone (DESIGN_SYSTEM_NOT_FOUND). */
    async identity(designSystemId) {
      try {
        const body = await api.post("/get_design_system", { design_system_id: designSystemId });
        return { name: body?.name ?? "", description: body?.description ?? "" };
      } catch (err) {
        if (err?.code === "DESIGN_SYSTEM_NOT_FOUND") return null;
        throw err;
      }
    },

    /** Saves the record as the next version; DESIGN_SYSTEM_CHANGED and DESIGN_SYSTEM_INVALID arrive as errors. Answers the new version. */
    async save(designSystemId, baseVersion, record, message) {
      const body = await api.post("/save_design_system", { design_system_id: designSystemId, base_version: baseVersion, record, message });
      return body.version;
    },

    /** Renames a saved token as its own version; answers the new version. */
    async rename(designSystemId, baseVersion, from, to) {
      const body = await api.post("/rename_design_token", {
        design_system_id: designSystemId,
        base_version: baseVersion,
        group: from.group,
        name: from.name,
        new_group: to.group,
        new_name: to.name,
      });
      return body.version;
    },

    /** One page of the version list, newest first; `before` is a version number, the page holds those older than it. */
    async versions(designSystemId, { limit, before } = {}) {
      const body = await api.post("/list_design_system_versions", { design_system_id: designSystemId, limit, before });
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
    async version(designSystemId, version) {
      const body = await api.post("/get_design_system_version", { design_system_id: designSystemId, version });
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
     * The project's repositories with the design system each uses, as rows a page edits, asked about the given design system.
     * BRIDGE (ticket 15): without one it is the project's first.
     */
    async repositories(projectId, designSystemId = "") {
      const id = designSystemId || (await firstOf(projectId));
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

    /** Applies the given design system (BRIDGE, ticket 15: the project's first when none is given) or removes the one in use. Answers what is stored. */
    async setRepository(repositoryId, uses, path, projectId, confirmMove = false, designSystemId = "") {
      const id = uses ? designSystemId || (await firstOf(projectId)) : "";
      if (uses && !id) throw new StructuredError(Codes.BAD_RESPONSE, "The project has no design system.");
      const stored = await this.apply(repositoryId, uses ? id : null, path, confirmMove);
      return { ...stored, uses: stored.designSystemId !== "" };
    },

    /** The project's design systems as the list shows them: identity, version, the repositories using each. */
    async list(projectId) {
      const body = await api.post("/list_design_systems", { project_id: projectId });
      return list(body?.design_systems).map(listRow);
    },

    /** Creates a design system with no version; answers its id. SLUG_TAKEN (details.field) and an invalid input arrive as errors. */
    async create(projectId, { name, slug, description }) {
      const body = await api.post("/create_design_system", { project_id: projectId, name, slug, description });
      return body.design_system_id;
    },

    /** Renames a design system or changes its description; only the given parts are sent, and the slug never changes. */
    async update(designSystemId, { name, description }) {
      await api.post("/update_design_system", {
        design_system_id: designSystemId,
        ...(name === undefined ? {} : { name }),
        ...(description === undefined ? {} : { description }),
      });
    },

    /** Deletes a design system after its name was typed; DESIGN_SYSTEM_IN_USE (details.repositories) arrives as an error. */
    async remove(designSystemId, confirmName) {
      await api.post("/delete_design_system", { design_system_id: designSystemId, confirm_name: confirmName });
    },

    /** Follows the project feed for the list: any design system of the project saved, created, renamed or deleted. */
    followProject(projectId, { onChanged, onStatus }) {
      return feed.follow(`/events?project_id=${encodeURIComponent(projectId)}`, {
        onMessage(dto) {
          const saved = dto?.design_system_saved;
          const changed = dto?.design_system_changed;
          if ((saved && saved.project_id === projectId) || (changed && changed.project_id === projectId)) onChanged?.();
        },
        onStatus,
      });
    },

    /**
     * Follows the project feed for what names this design system: saves of it and changes to its name or description
     * (or its deletion). Another design system's events raise nothing.
     */
    follow(projectId, designSystemId, { onSaved, onChanged, onStatus }) {
      return feed.follow(`/events?project_id=${encodeURIComponent(projectId)}`, {
        onMessage(dto) {
          const saved = dto?.design_system_saved;
          if (saved && saved.project_id === projectId && saved.design_system_id === designSystemId) {
            onSaved?.({ version: Number.isInteger(saved.version) ? saved.version : 0, source: saved.source ?? "" });
          }
          const changed = dto?.design_system_changed;
          if (changed && changed.project_id === projectId && changed.design_system_id === designSystemId) onChanged?.();
        },
        onStatus,
      });
    },
  };
}
