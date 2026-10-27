// The agent picker the new-session form and the brainstorm dialog share: it
// offers only the agents the project enables (ADR 0038), says so, and follows
// the page feed so the list is right while the form is open. The server paints
// the first list into data attributes on the component's element:
//
//   data-agents='[{"id":"a1","name":"reviewer","description":""}]'
//   data-agents-total="10" data-agents-mode="selected"
//   data-project-name="atlas-api" data-project-id="p1"
//
// Mixed into a component with withAgentPicker; the component calls
// startAgentPicker(this.$el.dataset) from init and stopAgentPicker from destroy,
// and hands a refused start to refuseAgent.

import { FeedStatus } from "../../../../shared/domain/feed.js";
import { agentsSettingsHref, catalogChangeTouches, notEnabledRefusal, pickerCount, pickerHint } from "../../domain/agentPicker.js";

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
    /** The AGENT_NOT_ENABLED banner, or null. */
    agentRefusal: null,

    startAgentPicker(dataset) {
      this.agents = readAgents(dataset.agents);
      this.agentTotal = Number(dataset.agentsTotal) || this.agents.length;
      this.agentMode = dataset.agentsMode === "selected" ? "selected" : "all";
      this.projectName = dataset.projectName ?? "";
      this.noteAgentNames(this.agents);
      if (!this.projectId) this.projectId = dataset.projectId ?? "";
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
    async refreshAgents() {
      const mine = ++asked;
      try {
        const choice = await gateway.agentChoices(this.projectId);
        if (mine !== asked) return;
        this.agents = choice.agents;
        this.agentTotal = choice.total;
        this.agentMode = choice.mode;
        this.noteAgentNames(choice.agents);
      } catch {
        // The list on screen stays; a start the server refuses says why.
      }
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
    get agentCount() {
      return pickerCount(this.agents.length);
    },
    get agentHint() {
      return pickerHint({ count: this.agents.length, total: this.agentTotal, mode: this.agentMode, project: this.projectName });
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

    /** A refused start: when it is AGENT_NOT_ENABLED, the banner, no agent selected, the list read again. Returns whether it was. */
    refuseAgent(err) {
      const refusal = notEnabledRefusal(err, { agent: this.chosenAgentName, project: this.projectName, projectId: this.projectId });
      if (refusal === null) return false;
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
