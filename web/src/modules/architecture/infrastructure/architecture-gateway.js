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

  /** One discovery per repository for the page's life: every tab and the matrix share the answer. */
  const catalogs = new Map();

  return {
    async get(repositoryId) {
      return toState(await api.post("/get_repository_architecture", { repository_id: repositoryId }));
    },

    /** What the harness found for the person to confirm; it never writes. */
    async discover(repositoryId) {
      const body = await api.post("/discover_repository_architecture", { repository_id: repositoryId });
      const list = (xs) => (Array.isArray(xs) ? xs : []);
      return {
        instructions: list(body?.instructions),
        tools: list(body?.tools),
        sandboxes: list(body?.sandboxes),
        agents: list(body?.agents),
      };
    },

    /** The newest rule events of a repository, newest first. */
    async listRuleEvents(repositoryId, limit = 50) {
      const body = await api.post("/list_architecture_rule_events", { repository_id: repositoryId, limit });
      return Array.isArray(body?.events) ? body.events : [];
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

    /** Commits the saved version as the architecture file; refusals (CHECKOUT_DIRTY, WRONG_BRANCH, ARCHITECTURE_CHANGED) arrive as errors. */
    async exportArchitecture(repositoryId, version) {
      const body = await api.post("/export_repository_architecture", { repository_id: repositoryId, version });
      return {
        commit: body?.commit ?? "",
        path: body?.path ?? "",
        export: body?.export ?? { state: "in_sync", path: body?.path ?? ".harness/architecture.yaml" },
      };
    },

    /**
     * What the layers of an unsaved draft would match: per layer a file count
     * and a sample (or the reason its pattern is invalid), the files in no
     * layer, and the overlaps with their winner. It never saves.
     */
    async previewLayers(repositoryId, layers, unlayered) {
      return readPreview(await api.post("/preview_layers", { repository_id: repositoryId, layers, unlayered }));
    },

    /** The agent catalog's names, for the matrix's rows and the agent pickers. Asked once per repository; a failure is not remembered. */
    agents(repositoryId) {
      if (!catalogs.has(repositoryId)) {
        const asked = this.discover(repositoryId).then((found) => found.agents.filter((a) => typeof a === "string"));
        asked.catch(() => catalogs.delete(repositoryId));
        catalogs.set(repositoryId, asked);
      }
      return catalogs.get(repositoryId);
    },

    /** The names of a project's bounded contexts, in the order the server lists them. */
    async boundedContexts(projectId) {
      const body = await api.get("/list_bounded_contexts", { project_id: projectId });
      const list = Array.isArray(body?.bounded_contexts) ? body.bounded_contexts : [];
      return list.map((c) => c?.name).filter((n) => typeof n === "string" && n !== "");
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
