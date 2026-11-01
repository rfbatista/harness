// The architecture assistant's side panel on a repository's Architecture page:
// a component nested in architecturePage, so `this.draft`, `this.version` and
// `this.repositoryId` reach the page's draft. It holds the conversation
// (its id, the messages) in the panel's state only; closing the panel or
// leaving the page ends the conversation on the server.
//
// A turn streams in: text as it is written and a progress line for each file
// the assistant reads. Stop is an abort. Nothing here saves anything.

import { describeError } from "../../../../shared/presentation/errors.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { ASSISTANT_CONVERSATION_NOT_FOUND, ASSISTANT_UNAVAILABLE, activityText, turnRequest } from "../../domain/assistant.js";
import { applyProposal, changeRow, groupChanges, proposalConflicts, proposalFigures, valueText } from "../../domain/proposal.js";
import { TABS, cleanRecord, describePointer, tabOfPointer } from "../../domain/record.js";
import { DRAFT_REPLACED } from "../pages/architecturePage.js";

/** @param {{ gateway: ReturnType<typeof import("../../infrastructure/architecture-gateway.js").architectureGateway> }} deps */
export const architectureAssistant = ({ gateway }) => () => ({
  /** What the page's seed says: whether the assistant can run, and if not why and where to fix it. */
  available: false,
  reason: "",
  fixUrl: "",
  open: false,
  input: "",
  /** The conversation: { id, who: "person" | "assistant", text } in order. */
  messages: [],
  /** The progress lines of the turn that runs: never kept after it. */
  activity: [],
  conversationId: "",
  running: false,
  /** The last turn's failure, shown in the panel; "" when there is none. */
  failure: "",
  /** Whether the server no longer knows the conversation. */
  lost: false,
  controller: null,
  counter: 0,
  /** The rule event the person asked about: { id, text }, sent with the next turn only. */
  quote: null,

  get unavailable() {
    return !this.available;
  },
  get canSend() {
    return !this.running && this.input.trim() !== "";
  },

  init() {
    const seed = readSeed(this.$el.closest("[data-seed]"));
    this.available = seed.assistant_available === true;
    this.reason = seed.assistant_reason ?? "";
    this.fixUrl = seed.assistant_fix_url ?? "";
  },

  /** "Ask the assistant" on a rule event: the panel opens with the event quoted and a question ready to send. */
  ask(event) {
    if (this.unavailable) return;
    const row = event.detail;
    this.quote = { id: row.id, text: `${row.agent} · ${row.rule} · ${row.word} · ${row.path}${row.layer ? ` · ${row.layer}` : ""}` };
    this.input = "Why did this rule fire, and how should I fix it?";
    this.show();
  },

  dropQuote() {
    this.quote = null;
  },

  toggle() {
    if (this.open) this.close();
    else this.show();
  },

  show() {
    if (this.unavailable) return;
    this.open = true;
    this.assistantOpen = true;
    this.$nextTick(() => this.$refs.input.focus());
  },

  /** Closing ends the conversation: the turn that runs is stopped and the server forgets the id. */
  close() {
    if (!this.open) return;
    this.open = false;
    this.assistantOpen = false;
    this.leave();
    this.reset();
  },

  /** Tells the server the conversation is over, as the page closes or unloads. */
  leave() {
    this.controller?.abort();
    gateway.endAssistantConversation(this.conversationId);
  },

  /** A new, empty conversation. */
  reset() {
    this.messages = [];
    this.activity = [];
    this.conversationId = "";
    this.running = false;
    this.failure = "";
    this.lost = false;
    this.input = "";
    this.quote = null;
    this.controller = null;
  },

  /** A new conversation after the server lost the old one; what was said stays on screen. */
  startAgain() {
    this.conversationId = "";
    this.lost = false;
    this.failure = "";
  },

  /** Enter sends; Shift+Enter is a new line. */
  onInputKey(event) {
    if (event.key !== "Enter" || event.shiftKey || event.isComposing) return;
    event.preventDefault();
    this.send();
  },

  onPanelKey(event) {
    if (event.key !== "Escape") return;
    event.stopPropagation();
    this.close();
  },

  add(who, text = "") {
    const message = { id: ++this.counter, who, text };
    this.messages.push(message);
    return message.id;
  },

  messageById(id) {
    return this.messages.find((m) => m.id === id);
  },

  async send() {
    if (!this.canSend) return;
    const text = this.input.trim();
    this.input = "";
    this.failure = "";
    this.lost = false;
    // The event rides with this turn only; later turns are the conversation's own.
    const quote = this.quote;
    this.quote = null;
    this.add("person", text);
    const replyId = this.add("assistant");
    this.activity = [];
    this.running = true;
    const controller = new AbortController();
    this.controller = controller;
    try {
      const result = await gateway.runAssistantTurn(
        turnRequest({
          repositoryId: this.repositoryId,
          conversationId: this.conversationId,
          message: text,
          draft: cleanRecord(this.draft),
          baseVersion: this.version,
          ruleEventId: quote?.id,
        }),
        {
          signal: controller.signal,
          onConversation: (id) => {
            this.conversationId = id;
          },
          onActivity: (a) => this.activity.push(activityText(a)),
          onDelta: (piece) => {
            this.messageById(replyId).text += piece;
          },
        },
      );
      // The result's text is the whole answer, authoritative over the deltas.
      this.messageById(replyId).text = result.text;
      if (result.proposal) this.propose(replyId, result.proposal);
    } catch (err) {
      this.turnEnded(replyId, err);
    } finally {
      if (this.controller === controller) {
        this.controller = null;
        this.running = false;
        this.activity = [];
      }
    }
  },

  /** Attaches a proposal to the answer that carried it and asks for the layer preview of the draft with it applied. */
  propose(messageId, proposal) {
    // A revised proposal replaces the ones still waiting for an answer.
    for (const m of this.messages) {
      if (m.proposal && (m.proposal.state === "open" || m.proposal.state === "conflicts")) m.proposal.state = "superseded";
    }
    this.messageById(messageId).proposal = {
      id: proposal.id,
      summary: proposal.summary,
      valid: proposal.valid === true,
      errors: Array.isArray(proposal.errors) ? proposal.errors.map(errorLine) : [],
      groups: groupChanges(proposal).map((g) => ({ slug: g.slug, label: g.label, rows: g.changes.map(changeRow) })),
      raw: proposal,
      /** "open"; "conflicts" while the person picks; then "accepted" or "discarded", or "superseded" by a newer proposal. */
      state: "open",
      /** The changes that touch what the person edited: { index, label, draft, proposed, choice } with choice "", "draft" or "proposal". */
      conflicts: [],
      /** "loading", "ready" or "failed", with the figures once ready. */
      preview: "loading",
      figures: null,
    };
    if (proposal.valid === true) this.previewProposal(messageId);
  },

  /** What the layers of the draft would match with the proposal applied; a failure only leaves the card without figures. */
  async previewProposal(messageId) {
    const proposal = this.messageById(messageId).proposal;
    const applied = applyProposal(this.draft, proposal.raw);
    try {
      const preview = await gateway.previewLayers(this.repositoryId, applied.layers, applied.unlayered);
      proposal.figures = proposalFigures(preview);
      proposal.preview = "ready";
    } catch {
      proposal.preview = "failed";
    }
  },

  /**
   * Puts the proposal into the page's unsaved draft, whole. A change that touches what the person edited
   * and has not saved first has to be settled: the card shows both values and waits. Nothing is saved.
   */
  accept(messageId) {
    const proposal = this.messageById(messageId).proposal;
    if (proposal.state !== "open" || !proposal.valid) return;
    const conflicts = proposalConflicts(this.draft, this.saved.record, proposal.raw);
    if (conflicts.length === 0) {
      this.land(proposal, proposal.raw);
      return;
    }
    proposal.conflicts = conflicts.map((c) => ({
      index: c.index,
      label: changeRow(c.change).label,
      draft: valueText(c.change, c.draftValue),
      proposed: valueText(c.change, c.proposedValue),
      choice: "",
    }));
    proposal.state = "conflicts";
  },

  /** The person's pick for one conflict: "draft" keeps their value, "proposal" takes the assistant's. */
  pick(messageId, index, choice) {
    this.messageById(messageId).proposal.conflicts.find((c) => c.index === index).choice = choice;
  },

  /** Whether every conflict of a proposal has a pick. */
  settled(proposal) {
    return proposal.conflicts.every((c) => c.choice !== "");
  },

  /** Lands the proposal once every conflict has a pick; a change whose conflict kept the draft value is left out. */
  settle(messageId) {
    const proposal = this.messageById(messageId).proposal;
    if (proposal.state !== "conflicts" || !this.settled(proposal)) return;
    const kept = new Set(proposal.conflicts.filter((c) => c.choice === "draft").map((c) => c.index));
    this.land(proposal, { ...proposal.raw, changes: proposal.raw.changes.filter((_, i) => !kept.has(i)) });
  },

  /** Back from the conflict view to the open card, the draft untouched. */
  unsettle(messageId) {
    const proposal = this.messageById(messageId).proposal;
    if (proposal.state !== "conflicts") return;
    proposal.conflicts = [];
    proposal.state = "open";
  },

  land(proposal, changes) {
    this.draft = applyProposal(this.draft, changes);
    proposal.state = "accepted";
    window.dispatchEvent(new CustomEvent(DRAFT_REPLACED));
  },

  /** What a card that is no longer waiting says. */
  outcomeText(state) {
    return { accepted: "Accepted into your unsaved changes.", discarded: "Discarded.", superseded: "Superseded by a newer proposal." }[state] ?? "";
  },

  discard(messageId) {
    const proposal = this.messageById(messageId).proposal;
    if (proposal.state === "open" || proposal.state === "conflicts") proposal.state = "discarded";
  },

  /** What a turn that did not finish leaves: a stopped turn keeps its text, a failed one says why. */
  turnEnded(replyId, err) {
    const reply = this.messageById(replyId);
    if (err?.name !== "AbortError") {
      if (!reply?.text) this.messages = this.messages.filter((m) => m.id !== replyId);
      if (err?.code === ASSISTANT_CONVERSATION_NOT_FOUND) {
        this.lost = true;
        return;
      }
      if (err?.code === ASSISTANT_UNAVAILABLE) {
        this.available = false;
        this.reason = err.message;
        this.fixUrl = err.details?.fix_url ?? "";
        this.close();
        return;
      }
      this.failure = describeError(err).message;
      return;
    }
    if (reply && !reply.text) this.messages = this.messages.filter((m) => m.id !== replyId);
  },

  stop() {
    this.controller?.abort();
  },
});

/** A validation error as the save bar words it: "Layers › Layers › 3 › match › regex: does not compile". */
function errorLine(e) {
  const tab = TABS.find((t) => t.slug === tabOfPointer(e.path));
  return `${tab.label} › ${describePointer(e.path)}: ${e.message}`;
}
