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
import { cleanRecord } from "../../domain/record.js";

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

  toggle() {
    if (this.open) this.close();
    else this.show();
  },

  show() {
    if (this.unavailable) return;
    this.open = true;
    this.$nextTick(() => this.$refs.input.focus());
  },

  /** Closing ends the conversation: the turn that runs is stopped and the server forgets the id. */
  close() {
    if (!this.open) return;
    this.open = false;
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
