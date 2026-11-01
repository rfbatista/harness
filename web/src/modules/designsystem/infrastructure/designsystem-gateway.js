// The design system module's gateway over /api and the project feed: the
// endpoints of "Contract: Harness server ↔ Web UI — project design system".
// The wire shapes are read here and nowhere else.

import { StructuredError, Codes } from "../../../shared/domain/errors.js";
import { listRow } from "../domain/identity.js";
import { sourceRow, previewView } from "../domain/import.js";
import { optInRow } from "../domain/repositories.js";
import { proposalRowDto, proposalViewDto } from "../domain/proposals.js";

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @param {import("../../../shared/infrastructure/feed.js").Feed} feed
 */
export function designSystemGateway(api, feed) {
  /** An import's request: the paths the person left empty are the server's defaults. */
  const importBody = (projectId, repositoryId, { designMd, tokens, components }) => ({
    project_id: projectId,
    repository_id: repositoryId,
    ...(designMd ? { design_md_path: designMd } : {}),
    ...(tokens ? { tokens_path: tokens } : {}),
    ...(components ? { components_dir: components } : {}),
  });

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
        repository: v.repository?.name ?? "",
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

    /** Every repository of the design system's project as rows a page edits, with the design system each uses. */
    async repositories(designSystemId) {
      const body = await api.post("/list_design_system_repositories", { design_system_id: designSystemId });
      return list(body?.repositories).map(optInRow);
    },

    /**
     * Exports the repository's design system into its checkout: one local commit, nothing pushed. Answers { commit, version, state }.
     * CHECKOUT_DIRTY, WRONG_BRANCH, NO_DESIGN_SYSTEM and EDITED_OUTSIDE (details.changed_files, unless confirmed) arrive as errors.
     */
    async exportDesignSystem(repositoryId, confirmEditedOutside = false) {
      const body = await api.post("/export_design_system", { repository_id: repositoryId, confirm_edited_outside: confirmEditedOutside });
      if (typeof body?.commit !== "string" || !Number.isInteger(body?.version)) {
        throw new StructuredError(Codes.BAD_RESPONSE, "The export came back unreadable.");
      }
      return { commit: body.commit, version: body.version, state: body.state ?? "" };
    },

    /** The files an export would write or delete at the repository's path, each with its diff against the checkout. */
    async previewExport(repositoryId) {
      const body = await api.post("/preview_design_system_export", { repository_id: repositoryId });
      return list(body?.files).map((f) => ({ path: f.path ?? "", status: f.status ?? "", diff: f.diff ?? "" }));
    },

    /** One repository's design system ("" for none) and the folder an export writes to. */
    async repository(repositoryId) {
      const body = await api.get("/get_repository", { repository_id: repositoryId });
      const repo = body?.repository;
      if (typeof repo?.id !== "string" || repo.id === "") {
        throw new StructuredError(Codes.BAD_RESPONSE, "The repository came back unreadable.");
      }
      return { id: repo.id, designSystemId: repo.design_system_id ?? "", path: repo.design_system_path ?? "" };
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

    /** Starts a design session on the design system; answers its session id. */
    async startDesignSession(designSystemId, agentId, prompt) {
      const body = await api.post("/start_design_session", { design_system_id: designSystemId, agent_id: agentId, prompt });
      return { sessionId: String(body.session_id ?? "") };
    },

    /** How many design sessions work on the design system now. */
    async runningDesignSessions(designSystemId) {
      const body = await api.post("/get_design_system", { design_system_id: designSystemId });
      return Number.isInteger(body.running_design_sessions) ? body.running_design_sessions : 0;
    },

    /** The project's design systems as the list shows them: identity, version, the repositories using each. */
    /** The design system's proposals, newest first, as the Proposals tab lists them. */
    async proposals(designSystemId) {
      const body = await api.post("/list_design_system_proposals", { design_system_id: designSystemId });
      return list(body?.proposals).map(proposalRowDto);
    },

    /** One proposal to review, with its items and the record as it would be if every item were approved. */
    async proposal(proposalId) {
      return proposalViewDto(await api.post("/get_design_system_proposal", { proposal_id: proposalId }));
    },

    /** Approves or rejects every item of an open proposal; answers the version it wrote (0 for none) and its state. */
    async decide(proposalId, itemIds, decision, confirmOverwrite = []) {
      const body = await api.post("/decide_design_system_proposal", {
        proposal_id: proposalId,
        decisions: itemIds.map((id) => ({ item_id: id, decision })),
        confirm_overwrite: confirmOverwrite,
      });
      return { version: Number.isInteger(body?.version) ? body.version : 0, state: body?.state ?? "" };
    },

    /** Drops a proposal whose session ended. */
    async withdraw(proposalId) {
      await api.post("/withdraw_design_system_proposal", { proposal_id: proposalId });
    },

    /** The record of a version the proposal started from; empty for version 0. */
    async baseRecord(designSystemId, version) {
      if (version === 0) return { tokens: [], principles: "", components: [] };
      return (await this.version(designSystemId, version)).record;
    },

    /** How many proposals of the project's design systems wait on the person. */
    async openProposalCount(projectId) {
      const body = await api.post("/list_design_systems", { project_id: projectId });
      return list(body?.design_systems).reduce((n, d) => n + (Number.isInteger(d.open_proposals) ? d.open_proposals : 0), 0);
    },

    /** Follows the project feed for the proposals of this design system and its saves. */
    followProposals(projectId, designSystemId, { onProposal, onStatus }) {
      return feed.follow(`/events?project_id=${encodeURIComponent(projectId)}`, {
        onMessage(dto) {
          const p = dto?.design_system_proposal;
          if (p && p.project_id === projectId) onProposal?.({ designSystemId: p.design_system_id, proposalId: p.proposal_id, state: p.state });
        },
        onStatus,
      });
    },

    async list(projectId) {
      const body = await api.post("/list_design_systems", { project_id: projectId });
      return list(body?.design_systems).map(listRow);
    },

    /** Creates a design system with no version; answers its id. SLUG_TAKEN (details.field) and an invalid input arrive as errors. */
    async create(projectId, { name, slug, description }) {
      const body = await api.post("/create_design_system", { project_id: projectId, name, slug, description });
      return body.design_system_id;
    },

    /** The project's repositories whose default branch holds a DESIGN.md or a tokens.json, with the paths found. */
    async importSources(projectId) {
      const body = await api.post("/find_design_system_sources", { project_id: projectId });
      return list(body?.repositories).map(sourceRow);
    },

    /** What importing would store, read from the repository's default branch; nothing is written. IMPORT_UNREADABLE arrives as an error. */
    async previewImport(projectId, repositoryId, paths) {
      return previewView(await api.post("/preview_design_system_import", importBody(projectId, repositoryId, paths)));
    },

    /** Creates a new design system from the repository's files; answers its id. SLUG_TAKEN and IMPORT_UNREADABLE arrive as errors. */
    async importDesignSystem(projectId, repositoryId, paths, { name, slug, description }) {
      const body = await api.post("/import_design_system", { ...importBody(projectId, repositoryId, paths), name, slug, description });
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
    follow(projectId, designSystemId, { onSaved, onChanged, onExport, onStatus }) {
      return feed.follow(`/events?project_id=${encodeURIComponent(projectId)}`, {
        onMessage(dto) {
          const saved = dto?.design_system_saved;
          if (saved && saved.project_id === projectId && saved.design_system_id === designSystemId) {
            onSaved?.({ version: Number.isInteger(saved.version) ? saved.version : 0, source: saved.source ?? "" });
          }
          const changed = dto?.design_system_changed;
          if (changed && changed.project_id === projectId && changed.design_system_id === designSystemId) onChanged?.();
          const exported = dto?.design_system_export;
          if (exported && exported.project_id === projectId && exported.design_system_id === designSystemId) onExport?.({ repositoryId: exported.repository_id ?? "", state: exported.state ?? "" });
        },
        onStatus,
      });
    },
  };
}
