// The architecture module's gateway over /api and the project feed: the
// endpoints of "Contract: Harness server ↔ Web UI — repository architecture
// record". The wire shapes are read here and nowhere else.

import { StructuredError, Codes } from "../../../shared/domain/errors.js";
import { readPreview } from "../domain/layers.js";

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @param {import("../../../shared/infrastructure/feed.js").Feed} feed
 */
export function architectureGateway(api, feed) {
  /** @param {any} body */
  const toState = (body) => {
    if (!body || typeof body.record !== "object" || body.record === null) {
      throw new StructuredError(Codes.BAD_RESPONSE, "The architecture came back unreadable.");
    }
    return {
      repositoryId: body.repository_id ?? "",
      exists: body.exists === true,
      version: Number.isInteger(body.version) ? body.version : 0,
      updatedAt: body.updated_at ?? "",
      record: body.record,
      export: body.export ?? { state: "never_exported", path: ".harness/architecture.yaml" },
    };
  };

  return {
    async get(repositoryId) {
      return toState(await api.post("/get_repository_architecture", { repository_id: repositoryId }));
    },

    /** Whether a repository has a saved architecture. */
    async has(repositoryId) {
      return (await this.get(repositoryId)).exists;
    },

    async save(repositoryId, baseVersion, record) {
      return toState(
        await api.post("/save_repository_architecture", {
          repository_id: repositoryId,
          base_version: baseVersion,
          record,
        }),
      );
    },

    /**
     * What the layers of an unsaved draft would match: per layer a file count
     * and a sample (or the reason its pattern is invalid), the files in no
     * layer, and the overlaps with their winner. It never saves.
     */
    async previewLayers(repositoryId, layers, unlayered) {
      return readPreview(await api.post("/preview_layers", { repository_id: repositoryId, layers, unlayered }));
    },

    /** The agent catalog's names, for the access matrix's rows. */
    async agents(repositoryId) {
      const body = await api.post("/discover_repository_architecture", { repository_id: repositoryId });
      return Array.isArray(body?.agents) ? body.agents.filter((a) => typeof a === "string") : [];
    },

    /**
     * Follows the project feed for one repository's architecture changes and
     * rule events. Other repositories' events and other kinds are ignored.
     */
    follow(projectId, repositoryId, { onChanged, onRuleEvent, onStatus }) {
      return feed.follow(`/events?project_id=${encodeURIComponent(projectId)}`, {
        onMessage(dto) {
          const changed = dto?.repository_architecture_changed;
          if (changed && changed.repository_id === repositoryId) {
            onChanged?.({ version: Number.isInteger(changed.version) ? changed.version : 0 });
            return;
          }
          const ev = dto?.architecture_rule_event;
          if (ev && ev.repository_id === repositoryId && ev.event) onRuleEvent?.(ev.event);
        },
        onStatus,
      });
    },
  };
}
