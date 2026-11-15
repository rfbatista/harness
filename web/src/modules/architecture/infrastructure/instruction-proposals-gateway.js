// The instruction-file proposals' gateway over /api and the project feed: the
// endpoints and the instruction_proposal_changed event of "Contract: Harness
// server ↔ Web UI — instruction-file proposals". The wire shapes are read here
// and nowhere else. Refusals arrive as StructuredErrors with the contract's
// codes (PROPOSAL_NOT_FOUND, PROPOSAL_DECIDED, INSTRUCTION_CHANGED, …).

import { proposalRow } from "../domain/instructionProposals.js";

const list = (xs) => (Array.isArray(xs) ? xs : []);

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @param {import("../../../shared/infrastructure/feed.js").Feed} feed
 */
export function instructionProposalsGateway(api, feed) {
  return {
    /** The repository's proposals, newest first, and how many are open. */
    async list(repositoryId) {
      const body = await api.post("/list_instruction_proposals", { repository_id: repositoryId });
      return { proposals: list(body?.proposals).map(proposalRow), openCount: Number.isInteger(body?.open_count) ? body.open_count : 0 };
    },

    /** Follows the project feed for one repository's proposals: { proposalId, state, openCount }. */
    follow(projectId, repositoryId, { onChanged, onStatus }) {
      return feed.follow(`/events?project_id=${encodeURIComponent(projectId)}`, {
        onMessage(dto) {
          const changed = dto?.instruction_proposal_changed;
          if (changed && changed.repository_id === repositoryId) {
            onChanged?.({
              proposalId: changed.proposal_id ?? "",
              state: changed.state ?? "",
              openCount: Number.isInteger(changed.open_count) ? changed.open_count : 0,
            });
          }
        },
        onStatus,
      });
    },
  };
}
