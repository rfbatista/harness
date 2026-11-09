// Sending and dismissing the open interviews, for the store
// "sessionsInterviews" (spread into it): where each interview is in its send
// (domain/sending.js), the dismiss note, what the footer says about it, and the
// calls to the gateway. A send or dismiss is in flight per interview id: a second
// press is the same one, and the feed's echo of it that beats the server's
// answer must neither move the card twice nor tell the person "another tab"
// did it (`inflight` is the store's to read for that). Whatever the answer is,
// the interview then leaves the open ones at once (`moveToEarlier`) or stays,
// its answers kept, with the reason.

import { canSend, failureOf, idle, reduce } from "../domain/sending.js";
import { firstBlank } from "../domain/answers.js";
import { footView } from "./interviewView.js";

/**
 * @param {{
 *   gateway: import("../domain/ports.js").InterviewGateway | null,
 *   inflight: Set<string>,  the ids being sent or dismissed here
 * }} deps
 */
export function sending({ gateway, inflight }) {
  return {
    /** The page stream's status, as the interview feed last said it: Send waits for "live". Without a gateway there is no stream to wait for. */
    connection: gateway ? "connecting" : "live",
    /** @type {Record<string, import("../domain/sending.js").Step>} where each interview is in its send, by interview id */
    steps: {},
    /** The note typed into the dismiss confirm, by interview id. */
    dismissNotes: {},
    /** What this page sent or dismissed, by interview id: the sent banner shows only where it was sent. */
    sentHere: {},

    step(id) {
      return this.steps[id] ?? idle;
    },

    dismissNoteOf(id) {
      return this.dismissNotes[id] ?? "";
    },

    typeDismissNote(id, text) {
      this.dismissNotes = { ...this.dismissNotes, [id]: text };
    },

    /** The footer as the markup binds it. */
    footer(id) {
      return footView(this.progress(id), this.step(id), { connection: this.connection, note: this.dismissNoteOf(id) });
    },

    /** Moves the interview along its steps. */
    advance(id, action) {
      this.steps = { ...this.steps, [id]: reduce(this.step(id), action) };
    },

    /** Moves it along, and says whether this is the action that began the call: one already in flight is the call, not a second. */
    begin(id, action) {
      const was = this.step(id).phase;
      this.advance(id, action);
      return was !== "sending" && this.step(id).phase === "sending";
    },

    /**
     * The Send button, and ⌘/Ctrl+Enter: blanks ask first, then the second press
     * (the strip's default button) sends. Nothing is sent while the stream is not live.
     * @param {(text: string) => void} [announce]
     */
    async send(sessionId, id, announce) {
      if (!this.openInterview(id) || !canSend(this.connection)) return;
      if (this.begin(id, { type: "send", skipped: this.progress(id).skipped })) await this.run(sessionId, id, announce);
    },

    /** Review skipped: back to the interview; answers the index of its first blank question. */
    review(id) {
      this.advance(id, { type: "review" });
      const interview = this.openInterview(id);
      return interview ? firstBlank(interview.questions, this.answersFor(id)) : -1;
    },

    askDismiss(id) {
      this.advance(id, { type: "dismiss" });
    },

    keepAnswering(id) {
      this.advance(id, { type: "keep" });
    },

    async confirmDismiss(sessionId, id, announce) {
      if (!this.openInterview(id) || !canSend(this.connection)) return;
      if (this.begin(id, { type: "confirm" })) await this.run(sessionId, id, announce);
    },

    /** Try again, after a failure that can be tried again: the same send or dismiss, which the server answers once. */
    async retry(sessionId, id, announce) {
      if (!canSend(this.connection)) return;
      if (this.begin(id, { type: "retry" })) await this.run(sessionId, id, announce);
    },

    /** The call itself, and what its answer does to the panel. */
    async run(sessionId, id, announce) {
      const kind = this.step(id).kind;
      inflight.add(id);
      try {
        const interview = kind === "send" ? await gateway.submit(id, this.submission(id)) : await gateway.dismiss(id, this.dismissNoteOf(id));
        this.settle(sessionId, id, interview, announce);
      } catch (err) {
        const error = failureOf(err);
        if (error.closed) this.settle(sessionId, id, error.closed, announce);
        else this.advance(id, { type: "failed", error });
      } finally {
        inflight.delete(id);
      }
    },

    /** The server's word on the interview: it leaves the open ones, opened to read, and the person is told. */
    settle(sessionId, id, interview, announce) {
      this.advance(id, { type: "done" });
      const { [id]: _, ...notes } = this.dismissNotes;
      this.dismissNotes = notes;
      if (interview.state === "sent" || interview.state === "expired") this.sentHere = { ...this.sentHere, [id]: true };
      const held = this.list(sessionId).interviews.find((i) => i.id === id);
      // The feed's echo may have come first, with a delivery that is newer than this answer's.
      const newer = held && held.state !== "open" && held.state === interview.state && held.delivery === "delivered";
      this.moveToEarlier(sessionId, newer ? held : interview, { open: true });
      announce?.(SAID[interview.state]);
    },
  };
}

const SAID = { sent: "Answers sent", dismissed: "Interview dismissed", expired: "Interview expired: the session ended" };
