// A repository's Agents tab: who may run in it, with Unassign and Assign again
// at once (no Save bar, ADR 0061). Nested in the Architecture page but not part
// of its draft.
//
//   <section x-data="repositoryAgentsTab" data-project-id="…" data-repository-id="…" data-layers-href="…">
//
// Every click sends the list version the page read. A stale click is refused
// with the current list: the page shows it, names what changed and applies
// nothing. The tab re-reads on repository_agents_changed and after a resync.

import { Codes } from "../../../../shared/domain/errors.js";
import { FeedStatus } from "../../../../shared/domain/feed.js";
import { describeError } from "../../../../shared/presentation/errors.js";
import { followSkeleton, skeletonFields } from "../../../../shared/presentation/skeleton.js";
import { compose } from "../../../agents/presentation/components/compose.js";
import { promptEditorPart } from "./promptEditor.js";
import { assignedToast, groupAgents, modeNotice, nothingAssigned, rowModel, staleBanner, stats, unassignedToast } from "../../domain/agentsTab.js";

/** @param {{ gateway: import("../../domain/ports.js").RepositoryAgentsGateway }} deps */
export const repositoryAgentsTab = ({ gateway }) => () => {
  let unfollow = () => {};
  /** When the first read shows a skeleton (shared rule). */
  let skeleton = null;
  return compose({
    ...skeletonFields(),
    projectId: "",
    repositoryId: "",
    layersHref: "",
    /** @type {import("../../domain/ports.js").RepositoryAgents | null} */
    list: null,
    loaded: false,
    busy: false,
    error: null,
    /** The banner of a refused stale click: { text }. */
    stale: null,
    /** @type {{ text: string, undo: string } | null} Undo names the agent to assign again. */
    toast: null,

    async init() {
      skeleton = followSkeleton(this);
      this.projectId = this.$el.dataset.projectId ?? "";
      this.repositoryId = this.$el.dataset.repositoryId ?? "";
      this.layersHref = this.$el.dataset.layersHref ?? "";
      unfollow = gateway.followChanges(
        (change) => {
          if (change.repositoryId !== this.repositoryId) return;
          this.reloadQuietly();
          this.promptMayHaveChanged(change.agents);
        },
        (status) => {
          if (status === FeedStatus.RESYNCED) {
            this.reloadQuietly();
            this.promptMayHaveChanged([this.editorAgent]);
          }
        },
      );
      await this.load();
      // A link from the agent catalog opens the editor on one agent: ?prompt=<agent>.
      const wanted = new URLSearchParams(globalThis.location?.search ?? "").get("prompt");
      if (wanted && this.list?.agents.some((a) => a.name === wanted)) await this.openPrompt(wanted);
    },

    destroy() {
      unfollow();
      skeleton?.stop();
    },

    async load() {
      if (!this.loaded) skeleton?.begin();
      try {
        this.list = await gateway.getRepositoryAgents(this.repositoryId);
        this.error = null;
        skeleton?.end();
      } catch (err) {
        skeleton?.fail();
        this.error = describeError(err);
      } finally {
        this.loaded = true;
      }
    },

    /** A re-read the feed asked for; our own write's echo is not re-read while the write is in flight. */
    async reloadQuietly() {
      if (this.busy) return;
      await this.load();
    },

    get hasTab() {
      return this.list !== null;
    },
    get groups() {
      if (!this.list) return { assigned: [], notAssigned: [], notEnabled: [] };
      const g = groupAgents(this.list);
      const rows = (list) => list.map((a) => rowModel(a, this.list));
      return { assigned: rows(g.assigned), notAssigned: rows(g.notAssigned), notEnabled: rows(g.notEnabled) };
    },
    get assignedRows() {
      return this.groups.assigned;
    },
    get notAssignedRows() {
      return this.groups.notAssigned;
    },
    get notEnabledRows() {
      return this.groups.notEnabled;
    },
    get notAssignedTitle() {
      return `Not assigned to ${this.list?.repository.name ?? ""} (${this.notAssignedRows.length})`;
    },
    get notEnabledTitle() {
      return `Not enabled in ${this.list?.project.name ?? ""} (${this.notEnabledRows.length})`;
    },
    get stats() {
      return this.list ? stats(this.list) : null;
    },
    get notice() {
      return this.list ? modeNotice(this.list) : { line: "", banner: null };
    },
    get nothingAssigned() {
      return this.list ? nothingAssigned(this.list) : null;
    },
    get repositoryName() {
      return this.list?.repository.name ?? "";
    },

    /** Runs one write; a stale refusal swaps in the current list and says what changed. @returns {Promise<import("../../domain/ports.js").RepositoryAgents | null>} */
    async write(run) {
      if (this.busy || !this.list) return null;
      this.busy = true;
      this.stale = null;
      const before = this.list;
      try {
        const next = await run(before.listVersion);
        this.list = next;
        this.error = null;
        return next;
      } catch (err) {
        if (err?.code === Codes.REPOSITORY_AGENTS_CHANGED && err.details?.current) {
          this.list = err.details.current;
          this.stale = { text: staleBanner(before, err.details.current) };
          this.toast = null;
        } else {
          this.error = describeError(err);
        }
        return null;
      } finally {
        this.busy = false;
      }
    },

    async unassign(agent) {
      const next = await this.write((listVersion) => gateway.unassignAgent({ repositoryId: this.repositoryId, agent, listVersion }));
      if (next) this.toast = { text: unassignedToast({ agent, repository: next.repository.name, running: next.runningSessions ?? 0 }), undo: agent };
    },

    async assign(agent) {
      const promptSet = this.list?.agents.find((a) => a.name === agent)?.prompt.set === true;
      const next = await this.write((listVersion) => gateway.assignAgentAgain({ repositoryId: this.repositoryId, agent, listVersion }));
      if (next) this.toast = { text: assignedToast({ agent, repository: next.repository.name, promptSet }), undo: "" };
    },

    async assignAll() {
      const next = await this.write((listVersion) => gateway.assignAllAgentsAgain({ repositoryId: this.repositoryId, listVersion }));
      if (next) this.toast = { text: `All enabled agents are assigned to ${next.repository.name} again.`, undo: "" };
    },

    /** The toast's Undo: assigns the agent again. */
    async undo() {
      const agent = this.toast?.undo;
      if (!agent) return;
      this.toast = null;
      await this.assign(agent);
    },

    /** The link under the title: the Architecture page's Layers tab, without leaving the page. */
    openLayers() {
      this.openTab("layers");
    },

    dismissToast() {
      this.toast = null;
    },
    dismissStale() {
      this.stale = null;
    },
    async reload() {
      this.stale = null;
      await this.load();
    },
  }, promptEditorPart({ gateway }));
};
