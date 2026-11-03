// Unassign and Assign again from an agent's detail, per repository (ADR 0061).
// The Agents tab of the repository settings is the main home of these; the
// detail is the other door. Each click sends the list version the catalog
// gave, a stale one is refused with the current list, and the catalog is read
// again either way (the page feed announces the change too).
//
// Mixed into agentsPage with compose; it needs the page's `item` (the open
// agent), `refresh` (the catalog's re-read) and `notice`.

import { Codes } from "../../../../shared/domain/errors.js";
import { describeError } from "../../../../shared/presentation/errors.js";

/** @param {{ repositories: Pick<import("../../../repositoryagents/domain/ports.js").RepositoryAgentsGateway, "unassignAgent" | "assignAgentAgain"> | undefined }} deps */
export function repositoryActions({ repositories }) {
  return {
    /** The key of the repository whose click is in flight; "" when none. */
    repoBusy: "",
    /** What the last click said when it was refused: { text }. */
    repoRefusal: null,

    /** @param {{ key: string, id: string, name: string, listVersion: string, unassigned: boolean }} repo */
    async toggleRepository(repo) {
      if (!repositories || this.repoBusy !== "") return;
      this.repoBusy = repo.key;
      this.repoRefusal = null;
      const input = { repositoryId: repo.id, agent: this.item, listVersion: repo.listVersion };
      try {
        if (repo.unassigned) {
          await repositories.assignAgentAgain(input);
          this.notice = `${this.item} is assigned to ${repo.name} again.`;
        } else {
          const after = await repositories.unassignAgent(input);
          const running = after.runningSessions ?? 0;
          this.notice = `Unassigned ${this.item} from ${repo.name}.${running > 0 ? ` ${running} running ${running === 1 ? "session continues" : "sessions continue"}.` : ""} It stays available in other repositories.`;
        }
      } catch (err) {
        if (err?.code === Codes.REPOSITORY_AGENTS_CHANGED) {
          this.repoRefusal = { text: `The agents of ${repo.name} changed while this page was open. Nothing was applied; the list below is up to date, so you can repeat it.` };
        } else {
          this.error = describeError(err);
        }
      } finally {
        this.repoBusy = "";
        try {
          await this.refresh();
        } catch (err) {
          this.error = describeError(err);
        }
      }
    },

    dismissRepoRefusal() {
      this.repoRefusal = null;
    },
  };
}
