// The instruction-file proposals' gateway over /api and the project feed: the
// endpoints and the instruction_proposal_changed event of "Contract: Harness
// server ↔ Web UI — instruction-file proposals". The wire shapes are read here
// and nowhere else. Refusals arrive as StructuredErrors with the contract's
// codes (PROPOSAL_NOT_FOUND, PROPOSAL_DECIDED, INSTRUCTION_CHANGED, …).

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { proposalFile, proposalRow } from "../domain/instructionProposals.js";

const list = (xs) => (Array.isArray(xs) ? xs : []);

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @param {import("../../../shared/infrastructure/feed.js").Feed} feed
 */
export function instructionProposalsGateway(api, feed) {
  /** Posts; an INSTRUCTION_CHANGED refusal gets the paths it listed. */
  const post = async (route, body) => {
    try {
      return await api.post(route, body);
    } catch (err) {
      if (err instanceof StructuredError && err.code === "INSTRUCTION_CHANGED") err.paths = list(err.details?.paths).filter((p) => typeof p === "string");
      throw err;
    }
  };
  const proposalOf = (body) => {
    if (!body?.proposal || typeof body.proposal.id !== "string") throw new StructuredError(Codes.BAD_RESPONSE, "The proposal came back unreadable.");
    return proposalRow(body.proposal);
  };

  return {
    /** The repository's proposals, newest first, and how many are open. */
    async list(repositoryId) {
      const body = await post("/list_instruction_proposals", { repository_id: repositoryId });
      return { proposals: list(body?.proposals).map(proposalRow), openCount: Number.isInteger(body?.open_count) ? body.open_count : 0 };
    },

    /** One proposal with its files: base and proposed content, versions and whether each is stale. */
    async get(proposalId) {
      const body = await post("/get_instruction_proposal", { proposal_id: proposalId });
      return { proposal: proposalOf(body), files: list(body.files).map(proposalFile) };
    },

    /** Approves the whole proposal: the decided proposal and the files saved (path and new version). INSTRUCTION_CHANGED, with `paths`, when a file is stale. */
    async approve(proposalId) {
      const body = await post("/approve_instruction_proposal", { proposal_id: proposalId });
      return { proposal: proposalOf(body), saved: list(body.files).map((f) => ({ path: f.path ?? "", version: Number.isInteger(f.version) ? f.version : 0 })) };
    },

    /** Rejects the proposal; the note is optional and told to the session. */
    async reject(proposalId, note) {
      const body = await post("/reject_instruction_proposal", note ? { proposal_id: proposalId, note } : { proposal_id: proposalId });
      return { proposal: proposalOf(body) };
    },

    /** Sends the proposal back to its session with a required note. */
    async sendBack(proposalId, note) {
      const body = await post("/send_back_instruction_proposal", { proposal_id: proposalId, note });
      return { proposal: proposalOf(body) };
    },

    /** Follows the project feed for every repository's proposals: { repositoryId, proposalId, state, openCount }. */
    followProject(projectId, { onChanged, onStatus }) {
      return feed.follow(`/events?project_id=${encodeURIComponent(projectId)}`, {
        onMessage(dto) {
          const changed = dto?.instruction_proposal_changed;
          if (changed && typeof changed.repository_id === "string") {
            onChanged?.({
              repositoryId: changed.repository_id,
              proposalId: changed.proposal_id ?? "",
              state: changed.state ?? "",
              openCount: Number.isInteger(changed.open_count) ? changed.open_count : 0,
            });
          }
        },
        onStatus,
      });
    },

    /** Follows the project feed for one repository's proposals: { proposalId, state, openCount }. */
    follow(projectId, repositoryId, { onChanged, onStatus }) {
      return this.followProject(projectId, {
        onChanged({ repositoryId: from, ...change }) {
          if (from === repositoryId) onChanged?.(change);
        },
        onStatus,
      });
    },
  };
}
