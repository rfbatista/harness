// The agent picker the new-session form and the brainstorm dialog share: it
// offers only the agents the project enables (ADR 0038), says so, and follows
// the page feed so the list is right while the form is open. The server paints
// the first list into data attributes on the component's element:
//
//   data-agents='[{"id":"a1","name":"reviewer","description":""}]'
//   data-agents-total="10" data-agents-mode="selected"
//   data-project-name="atlas-api" data-project-id="p1"
//
// With a repository chosen (the component's repositoryId) it offers only the
// agents assigned there (ADR 0061), lists the missing ones behind a
// disclosure with the reason, and reads again when the repository changes. The
// server paints the repository names too:
//
//   data-repositories='[{"id":"r1","name":"atlas-api"}]'
//
// Mixed into a component with withAgentPicker; the component calls
// startAgentPicker(this.$el.dataset) from init and stopAgentPicker from destroy,
// and hands a refused start to refuseAgent. A component may define
// agentsChanged(), called after the list was read again.

import { FeedStatus } from "../../../../shared/domain/feed.js";
import { agentsSettingsHref, catalogChangeTouches, notAssignedRefusal, notEnabledRefusal, notOfferedRows, pickerCount, pickerHint, repositoryAgentsHref, unassignedCount } from "../../domain/agentPicker.js";

function readNames(raw) {
  try {
    const list = JSON.parse(raw ?? "[]");
    return Array.isArray(list) ? Object.fromEntries(list.filter((r) => typeof r?.id === "string" && typeof r?.name === "string").map((r) => [r.id, r.name])) : {};
  } catch {
    return {};
  }
}

function readAgents(raw) {
  try {
    const list = JSON.parse(raw ?? "[]");
    return Array.isArray(list) ? list.filter((a) => typeof a?.id === "string" && typeof a?.name === "string") : [];
  } catch {
    return [];
  }
}

/**
 * Adds the picker's state, getters and methods to `component`, which has an
 * `agentId` and a `projectId`. Getters are copied as getters.
 * @param {object} component
 * @param {{ gateway: import("../../domain/ports.js").SessionGateway }} deps
 */
export function withAgentPicker(component, { gateway }) {
  let unfollow = () => {};
  let asked = 0;
  let live = false;
  const mixin = {
    /** The agents the project enables: { id, name, description }. */
    agents: [],
    agentTotal: 0,
    /** "all" or "selected". */
    agentMode: "all",
    projectName: "",
    /** Names seen in any list, so a choice that was taken off the list keeps its name. */
    agentNames: {},
    /** The AGENT_NOT_ENABLED or AGENT_NOT_ASSIGNED banner, or null. */
    agentRefusal: null,
    /** With a repository chosen: the catalog's agents it does not offer, with the reason: { name, reason }. */
    notOffered: [],
    /** Repository id → its name, painted by the server. */
    repositoryNames: {},
    /** Why the agent field went back to Plain claude after the repository changed; "" when it did not. */
    agentNote: "",

    startAgentPicker(dataset) {
      this.agents = readAgents(dataset.agents);
      this.agentTotal = Number(dataset.agentsTotal) || this.agents.length;
      this.agentMode = dataset.agentsMode === "selected" ? "selected" : "all";
      this.projectName = dataset.projectName ?? "";
      this.noteAgentNames(this.agents);
      this.repositoryNames = readNames(dataset.repositories);
      if (!this.projectId) this.projectId = dataset.projectId ?? "";
      // Another repository offers other agents: read the list again, and drop a choice it does not offer.
      this.$watch?.("repositoryId", () => void this.refreshAgents({ resetChoice: true }));
      unfollow = gateway.followAgents?.({
        onEvent: (data) => {
          if (catalogChangeTouches(data, this.projectId)) void this.refreshAgents();
        },
        // Live for the first time reads once more: an event between the page's paint and the feed's start is not missed.
        onStatus: (status) => {
          if (status === FeedStatus.RESYNCED || (status === FeedStatus.LIVE && !live)) void this.refreshAgents();
          if (status === FeedStatus.LIVE) live = true;
        },
      }) ?? (() => {});
    },
    stopAgentPicker() {
      unfollow();
      live = false;
      asked++;
    },
    noteAgentNames(list) {
      const names = { ...this.agentNames };
      for (const a of list) names[a.id] = a.name;
      this.agentNames = names;
    },

    /** Reads what the project enables now; an answer to a superseded question is dropped, a failed read keeps the list. */
    async refreshAgents({ resetChoice = false } = {}) {
      const mine = ++asked;
      try {
        const choice = await gateway.agentChoices(this.projectId, this.repositoryId || "");
        if (mine !== asked) return;
        this.agents = choice.agents;
        this.agentTotal = choice.total;
        this.agentMode = choice.mode;
        this.notOffered = choice.notOffered ?? [];
        this.noteAgentNames(choice.agents);
        if (resetChoice) this.resetUnofferedChoice();
        this.agentsChanged?.();
      } catch {
        // The list on screen stays; a start the server refuses says why.
      }
    },

    /** A repository that does not offer the chosen agent puts the field back on Plain claude, and says why. */
    resetUnofferedChoice() {
      this.agentNote = "";
      if (this.agentId === "" || this.agents.some((a) => a.id === this.agentId)) return;
      const name = this.chosenAgentName;
      this.agentId = "";
      this.agentNote = `${name} is not assigned to ${this.repositoryName}, so the agent is back on Plain claude.`;
    },

    /** The chosen agent's name, "" for plain claude. */
    get chosenAgentName() {
      return this.agentId === "" ? "" : (this.agentNames[this.agentId] ?? this.agentId);
    },
    /** What the select offers: the enabled agents, and the choice that was taken off the list while the form was open, marked. */
    get agentChoices() {
      const listed = this.agents.map((a) => ({ id: a.id, name: a.name, description: a.description }));
      if (this.agentId === "" || listed.some((a) => a.id === this.agentId)) return listed;
      return [...listed, { id: this.agentId, name: `${this.chosenAgentName} (no longer enabled)`, description: "Start will be refused: this agent is not enabled in the project" }];
    },
    /** The picker's agents as the select draws them: a repository prompt is marked in the description. */
    get agentOptions() {
      return this.agentChoices.map((a) => {
        const set = this.agents.find((x) => x.id === a.id)?.repositoryPromptSet === true;
        return { ...a, description: set ? [a.description, "repository prompt set"].filter(Boolean).join(" · ") : a.description };
      });
    },
    get repositoryName() {
      return this.repositoryNames[this.repositoryId] ?? "";
    },
    get agentCount() {
      return pickerCount(this.agents.length, this.repositoryName ? unassignedCount(this.notOffered) : 0);
    },
    get agentHint() {
      return pickerHint({ count: this.agents.length, total: this.agentTotal, mode: this.agentMode, project: this.projectName, repository: this.repositoryName, unassigned: unassignedCount(this.notOffered) });
    },
    /** "Not offered in <repository> (N)": the missing agents, each with its reason and the link that fixes it. */
    get notOfferedList() {
      if (!this.repositoryName) return [];
      return notOfferedRows(this.notOffered, { repository: this.repositoryName, repositoryId: this.repositoryId, project: this.projectName, projectId: this.projectId });
    },
    get hasNotOffered() {
      return this.notOfferedList.length > 0;
    },
    get notOfferedTitle() {
      return `Not offered in ${this.repositoryName} (${this.notOfferedList.length})`;
    },
    get hasAgentNote() {
      return this.agentNote !== "";
    },
    get repositoryAgentsLink() {
      return repositoryAgentsHref(this.projectId, this.repositoryId);
    },
    /** A Selected list with nothing in it: nothing can be picked, so Start is off. */
    get noAgentEnabled() {
      return this.agents.length === 0 && this.agentMode === "selected";
    },
    get hasAgentRefusal() {
      return this.agentRefusal !== null;
    },
    get agentsSettingsLink() {
      return agentsSettingsHref(this.projectId);
    },
    get noAgentLine() {
      return `No agent is enabled in ${this.projectName}.`;
    },

    /** A refused start: when it is AGENT_NOT_ENABLED or AGENT_NOT_ASSIGNED, the banner, no agent selected, the list read again. Returns whether it was. */
    refuseAgent(err) {
      const refusal =
        notEnabledRefusal(err, { agent: this.chosenAgentName, project: this.projectName, projectId: this.projectId }) ??
        notAssignedRefusal(err, { agent: this.chosenAgentName, repository: this.repositoryName, repositoryId: this.repositoryId, projectId: this.projectId });
      if (refusal === null) return false;
      this.agentNote = "";
      this.agentRefusal = refusal;
      this.agentId = "";
      void this.refreshAgents();
      return true;
    },
    /** Pick another agent: clears the banner and puts the focus on the picker. */
    pickAnotherAgent() {
      this.agentRefusal = null;
      this.$refs.agentField?.querySelector("button, select")?.focus();
    },
  };
  Object.defineProperties(component, Object.getOwnPropertyDescriptors(mixin));
  return component;
}
