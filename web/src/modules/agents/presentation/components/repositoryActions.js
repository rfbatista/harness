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
import { repositoriesOpen, repositoryToggled } from "../../domain/enabledIn.js";

/** @param {{ repositories: Pick<import("../../../repositoryagents/domain/ports.js").RepositoryAgentsGateway, "unassignAgent" | "assignAgentAgain"> | undefined }} deps */
export function repositoryActions({ repositories }) {
  return {
    /** The key of the repository whose click is in flight; "" when none. */
    repoBusy: "",
    /** What the last click said when it was refused: { text }. */
    repoRefusal: null,
    /** Project id → whether the person opened (true) or closed (false) its repositories; unset follows the default. */
    reposChosen: {},

    /** The repositories of a project row that show now. */
    reposShown(row) {
      return repositoriesOpen(row, this.reposChosen[row.id]) ? row.repositories : [];
    },
    reposExpanded(row) {
      return repositoriesOpen(row, this.reposChosen[row.id]);
    },
    toggleRepos(row) {
      this.reposChosen = { ...this.reposChosen, [row.id]: !this.reposExpanded(row) };
    },

    /** Whether a click is in flight: a boolean, since Alpine reads "" as set for a boolean attribute. */
    get repoIsBusy() {
      return this.repoBusy !== "";
    },

    /** @param {{ key: string, id: string, name: string, listVersion: string, unassigned: boolean }} repo */
    async toggleRepository(repo) {
      if (!repositories || this.repoBusy !== "") return;
      this.repoBusy = repo.key;
      this.repoRefusal = null;
      const input = { repositoryId: repo.id, agent: this.item, listVersion: repo.listVersion };
      try {
        const after = repo.unassigned ? await repositories.assignAgentAgain(input) : await repositories.unassignAgent(input);
        this.notice = repositoryToggled({ agent: this.item, repository: repo.name, unassigned: repo.unassigned, running: after.runningSessions ?? 0 });
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
