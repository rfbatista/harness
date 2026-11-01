// The design system module's gateway over /api and the project feed: the
// endpoints of "Contract: Harness server ↔ Web UI — project design system".
// The wire shapes are read here and nowhere else.

import { StructuredError, Codes } from "../../../shared/domain/errors.js";

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

  return {
    /** The state the server embedded in the page. */
    fromSeed: toState,

    async get(projectId) {
      return toState(await api.post("/get_design_system", { project_id: projectId }));
    },

    /** Saves the record as the next version; DESIGN_SYSTEM_CHANGED and DESIGN_SYSTEM_INVALID arrive as errors. Answers the new version. */
    async save(projectId, baseVersion, record, message) {
      const body = await api.post("/save_design_system", { project_id: projectId, base_version: baseVersion, record, message });
      return Number.isInteger(body?.version) ? body.version : baseVersion + 1;
    },

    /** Renames a saved token as its own version; answers the new version. */
    async rename(projectId, baseVersion, from, to) {
      const body = await api.post("/rename_design_token", {
        project_id: projectId,
        base_version: baseVersion,
        group: from.group,
        name: from.name,
        new_group: to.group,
        new_name: to.name,
      });
      return Number.isInteger(body?.version) ? body.version : baseVersion + 1;
    },

    /** One page of the version list, newest first; `before` is a version number, the page holds those older than it. */
    async versions(projectId, { limit, before } = {}) {
      const body = await api.post("/list_design_system_versions", { project_id: projectId, limit, before });
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
      const body = await api.post("/get_design_system_version", { project_id: projectId, version });
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
