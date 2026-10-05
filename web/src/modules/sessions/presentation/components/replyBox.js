// The developer's reply to the selected session. The page binds the session
// in; the box reports back with `reply-sent`.
//
//   <form x-data="sessionsReplyBox" x-modelable="sessionId" x-model="selectedId"
//         @submit.prevent="send">
//     <input x-model="draft" :disabled="cannotType"> <button :disabled="cannotSend">

import { Codes, codeOf } from "../../../../shared/domain/errors.js";
import { describeError } from "../../../../shared/presentation/errors.js";

/** @param {{ gateway: import("../../domain/ports.js").SessionGateway }} deps */
export const replyBox = ({ gateway }) => () => ({
  sessionId: null,
  draft: "",
  sending: false,
  closed: false,
  error: null,

  init() {
    // A different session: its own conversation, so a fresh start.
    this.$watch("sessionId", () => {
      this.closed = false;
      this.error = null;
    });
  },

  get cannotType() {
    return !this.sessionId || this.sending || this.closed;
  },
  get cannotSend() {
    return this.cannotType || this.draft.trim() === "";
  },

  async send() {
    if (this.cannotSend) return;
    const text = this.draft.trim();
    this.sending = true;
    this.error = null;
    try {
      await gateway.send(this.sessionId, text);
      this.draft = "";
      this.$dispatch("reply-sent", { sessionId: this.sessionId });
    } catch (err) {
      if (codeOf(err) === Codes.SESSION_NOT_RUNNING) this.closed = true;
      this.error = describeError(err);
    } finally {
      this.sending = false;
    }
  },
});
