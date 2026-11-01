// The architecture module's gateway over /api and the project feed: the
// endpoints of "Contract: Harness server ↔ Web UI — repository architecture
// record". The wire shapes are read here and nowhere else.

import { StructuredError, Codes } from "../../../shared/domain/errors.js";
import { sseParser } from "../../../shared/infrastructure/sse.js";
import { readPreview } from "../domain/layers.js";
import { instructionFilesGateway } from "./instruction-files-gateway.js";

import { ASSISTANT_FAILED } from "../domain/assistant.js";

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @param {import("../../../shared/infrastructure/feed.js").Feed} feed
 * @param {{ base?: string, fetch?: typeof globalThis.fetch, beacon?: (url: string, data: Blob) => boolean }} [transport] the assistant's streamed turn and end beacon
 */
export function architectureGateway(api, feed, { base = "/api", fetch = globalThis.fetch?.bind(globalThis), beacon = (url, data) => navigator.sendBeacon(url, data) } = {}) {
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

  /** One discovery per repository for the page life: every tab and the matrix share the answer (see discovered). */
  const catalogs = new Map();

  /** The catalog agent names, from the shared discovery. */
  const catalogNames = (repositoryId) => self.discovered(repositoryId).then((found) => found.agents.filter((a) => typeof a === "string"));
  /** @type {Map<string, Promise<{ offered: string[], notEnabled: string[] }>>} */
  const offered = new Map();
  const names = (body) => (Array.isArray(body?.agents) ? body.agents.map((a) => a?.name).filter((n) => typeof n === "string") : []);
  const offers = (repositoryId, projectId) => {
    const key = `${repositoryId}\n${projectId}`;
    if (!offered.has(key)) {
      const asked = (async () => {
        const catalog = await catalogNames(repositoryId);
        if (!projectId) return { offered: catalog, notEnabled: [] };
        let enabled;
        try {
          enabled = names(await api.get("/list_agents", { project_id: projectId }));
        } catch {
          return { offered: catalog, notEnabled: [] }; // the pickers fall back to the catalog
        }
        const set = new Set(enabled);
        return { offered: enabled, notEnabled: catalog.filter((n) => !set.has(n)) };
      })();
      asked.catch(() => offered.delete(key));
      offered.set(key, asked);
    }
    return offered.get(key);
  };

  const self = {
    /** The repository's instruction files: their own endpoints and feed event. */
    instructionFiles: instructionFilesGateway(api, feed),

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

    /**
     * One turn of the architecture assistant (POST /api/run_architecture_assistant_turn). The answer
     * streams as SSE-framed events: `conversation` and `activity` and `delta` go to the callbacks, and
     * the promise answers with the `result` ({ text, proposal }). A refusal before the stream, or an
     * `error` event, rejects with its code. Aborting the signal is Stop and rejects with an AbortError.
     */
    async runAssistantTurn(request, { signal, onConversation, onActivity, onDelta } = {}) {
      let res;
      try {
        res = await fetch(`${base}/run_architecture_assistant_turn`, {
          method: "POST",
          signal,
          headers: { "Content-Type": "application/json", Accept: "text/event-stream" },
          body: JSON.stringify(request),
        });
      } catch (err) {
        if (err?.name === "AbortError") throw err;
        throw new StructuredError(Codes.NETWORK, "The harness server is not reachable.");
      }
      if (!res.ok) throw await assistantRefusal(res);
      if (!res.body) throw assistantFailure("The assistant sent no answer.");
      return readAssistantStream(res.body, { onConversation, onActivity, onDelta });
    },

    /** Ends a conversation with a beacon, which survives the page unloading. The server answers 204 either way. */
    endAssistantConversation(conversationId) {
      if (!conversationId) return;
      beacon(`${base}/end_architecture_assistant_conversation`, new Blob([JSON.stringify({ conversation_id: conversationId })], { type: "application/json" }));
    },

    /**
     * The directory tree of a checkout (GET /api/list_tree rooted at it): nodes of
     * {path, is_dir, children}, paths relative to the root.
     */
    async listTree(rootDir) {
      const body = await api.get("/list_tree", { root: rootDir });
      if (!body || typeof body.tree !== "object" || body.tree === null) {
        throw new StructuredError(Codes.BAD_RESPONSE, "The folders came back unreadable.");
      }
      return body.tree;
    },

    /** What discovery found, asked once per repository for the page's life and shared: the agent pickers and the Instructions tab's missing marks read it. A failure is not remembered. */
    discovered(repositoryId) {
      if (!catalogs.has(repositoryId)) {
        const asked = this.discover(repositoryId);
        asked.catch(() => catalogs.delete(repositoryId));
        catalogs.set(repositoryId, asked);
      }
      return catalogs.get(repositoryId);
    },

    /**
     * The agent names the matrix's rows and the agent pickers offer: with a
     * project, the agents it enables (ADR 0038; list_agents?project_id=), so a
     * layer or rule is never assigned an agent the project cannot run; without
     * one, or when that read fails, the whole catalog. Asked once per repository
     * and project; a failure is not remembered.
     */
    agents(repositoryId, projectId = "") {
      return offers(repositoryId, projectId).then((o) => o.offered);
    },

    /** The catalog agents the project does not enable, so a stray the record names can say why it is not offered. */
    notEnabledAgents(repositoryId, projectId = "") {
      return offers(repositoryId, projectId).then((o) => o.notEnabled);
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
  return self;
}

const assistantFailure = (message) => new StructuredError(ASSISTANT_FAILED, message);

/** The JSON error a refused turn answers with, as a StructuredError (`fix_url` rides in the details). */
async function assistantRefusal(res) {
  let body = null;
  try {
    body = JSON.parse(await res.text());
  } catch {
    // not JSON: the status line will do
  }
  return new StructuredError(
    body?.code ?? Codes.UNKNOWN,
    body?.message ?? body?.error ?? `${res.status} ${res.statusText}`.trim(),
    res.status,
    body?.fix_url ? { fix_url: body.fix_url } : body?.details,
  );
}

/** @param {ReadableStream<Uint8Array>} body */
async function readAssistantStream(body, { onConversation, onActivity, onDelta }) {
  /** @type {{ text: string, proposal: any } | undefined} */
  let result;
  /** @type {StructuredError | undefined} */
  let error;
  const parser = sseParser((name, data) => {
    let payload;
    try {
      payload = JSON.parse(data);
    } catch {
      error ??= assistantFailure("The assistant sent an answer that cannot be read.");
      return;
    }
    if (payload === null || typeof payload !== "object") {
      error ??= assistantFailure("The assistant sent an answer that cannot be read.");
      return;
    }
    if (name === "conversation") onConversation?.(String(payload.conversation_id ?? ""));
    else if (name === "activity") onActivity?.({ tool: String(payload.tool ?? ""), target: String(payload.target ?? "") });
    else if (name === "delta") onDelta?.(String(payload.text ?? ""));
    else if (name === "result") result = { text: String(payload.text ?? ""), proposal: payload.proposal ?? null };
    else if (name === "error") error = new StructuredError(payload.code ?? ASSISTANT_FAILED, payload.message ?? "The assistant failed.");
  });

  const reader = body.getReader();
  const decoder = new TextDecoder();
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      parser.push(decoder.decode(value, { stream: true }));
    }
    parser.end();
  } catch (err) {
    if (err?.name === "AbortError") throw err;
    throw assistantFailure("The connection to the assistant was lost.");
  }
  if (error) throw error;
  if (!result) throw assistantFailure("The assistant stopped before it finished.");
  return result;
}
