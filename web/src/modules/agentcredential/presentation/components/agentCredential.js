// The Agent credential section of the Agent isolation page: whether agents
// have a credential, its kind, and pasting, replacing and removing it. The
// server paints the first view in [data-ssr] and embeds the seed; this takes
// over.
//
//   <section id="credential" x-data="agentCredential" data-seed="credential-seed">
//
// The value is typed into an uncontrolled password input, sent once, and
// cleared on save, cancel and pagehide. It is never held in component state
// and never shown again.
//
// Who agents are signed in as comes from `claude auth status`, run on the
// server under the sandbox driver: read when the page loads (the seed), after
// a sign-in and after a sign out, never polled and never taken from the login
// file. A status that cannot be read is "could not check". The page shows the
// account claude prints and nothing of the login. Signed in, the pasted
// credential is the secondary option, collapsed behind a button.
//
// Sign in with Claude runs `claude auth login` on the server, in the agents'
// own Claude home. The flow's state lives there, so the seed carries it and a
// page that was left finds the same panel; while it runs the page counts the
// time down and asks the server how it is going. Claude keeps the login; the
// pasted code is not held here.

import { describeError } from "../../../../shared/presentation/errors.js";
import { readSeed } from "../../../../shared/presentation/seed.js";

const KIND_NAMES = { oauth: "OAuth token", api_key: "Anthropic API key" };
const ACTIVE = ["starting", "waiting_for_code", "checking"];
/** How often the page asks the server how the flow is going, in ticks of a second. */
const POLL_EVERY = 2;

/** "9:05" for 545 seconds. */
export function clock(seconds) {
  const s = Math.max(0, Math.floor(seconds));
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
}

/** The kind a pasted value's prefix says it is, or "" when it does not say. */
export function kindOf(value) {
  const v = value.trim();
  if (v.startsWith("sk-ant-oat")) return "oauth";
  if (v.startsWith("sk-ant-api")) return "api_key";
  return "";
}

/** @param {{ gateway: import("../../domain/ports.js").AgentCredentialGateway }} deps */
export const agentCredential = ({ gateway }) => () => ({
  isSet: false,
  kind: "",
  updated: "",
  /** The kind chosen in the paste form. */
  chosen: "oauth",
  pasting: false,
  confirming: false,
  busy: false,
  error: null,
  announcement: "",
  canSignIn: false,
  /** @type {import("../../domain/ports.js").OAuthPhase} */
  phase: "idle",
  oauthUrl: "",
  oauthMessage: "",
  oauthDetail: "",
  remaining: 0,
  ticks: 0,
  /** This page saw the flow finish: the success banner stays until it is left. */
  signInSaved: false,
  timer: 0,
  /** `claude auth status`: an account is signed in. */
  signedIn: false,
  account: "",
  subscription: "",
  /** False when the status could not be read. */
  checkedOk: true,
  authChecking: false,
  /** The pasted credential's part is open; it is shut while signed in. */
  secondaryOpen: true,

  init() {
    const seed = readSeed(this.$el);
    this.setAuth(seed.auth);
    this.apply(seed);
    this.canSignIn = seed.can_sign_in === true;
    // A flow that finished before this page loaded is not news; a failure is.
    if (seed.oauth && seed.oauth.phase !== "done") this.applyOAuth(seed.oauth);
    for (const node of this.$el.querySelectorAll("[data-ssr]")) node.remove();
  },
  destroy() {
    this.stopTimer();
  },

  /** @param {import("../../domain/ports.js").CredentialStatus} status */
  apply(status) {
    this.isSet = status.set;
    this.kind = status.kind;
    this.updated = status.updated;
    this.$dispatch("isolation-chip", { section: "credential", ...this.summary });
  },

  /**
   * What `claude auth status` said. The pasted credential's part shuts when
   * an account is signed in and opens when none is.
   * @param {import("../../domain/ports.js").AuthStatus | undefined} auth
   */
  setAuth(auth) {
    this.signedIn = auth?.signed_in === true;
    this.account = auth?.account ?? "";
    this.subscription = auth?.subscription ?? "";
    this.checkedOk = auth ? auth.checked_ok !== false : true;
    this.secondaryOpen = !this.signedIn || this.pasting;
    this.$dispatch("isolation-chip", { section: "credential", ...this.summary });
  },

  /** The page's summary chip for this section. */
  get summary() {
    if (this.signedIn) return { text: "Credential: signed in", tone: "signal", blocks: false };
    return this.isSet
      ? { text: "Credential: set", tone: "signal", blocks: false }
      : { text: "Credential: missing", tone: "danger", blocks: true };
  },

  get notSet() {
    return !this.isSet && !this.signedIn;
  },
  get chipText() {
    if (this.signedIn) return "Signed in";
    return this.isSet ? "Set" : "Not set";
  },
  get chipTone() {
    return this.isSet || this.signedIn ? "signal" : "danger";
  },
  get hasSubscription() {
    return this.subscription !== "";
  },
  get notSignedInLine() {
    return !this.signedIn && this.checkedOk && !this.authChecking && this.canSignIn;
  },
  get couldNotCheck() {
    return !this.checkedOk && !this.authChecking;
  },
  get signInShown() {
    return !this.signedIn;
  },
  get signOutDisabled() {
    return this.busy || this.signingIn;
  },
  get secondaryExpanded() {
    return String(this.secondaryOpen);
  },
  get kindName() {
    return KIND_NAMES[this.kind] ?? "";
  },
  get savedDate() {
    return this.updated.slice(0, 10);
  },
  get pasteLabel() {
    return this.isSet ? "Replace with a pasted token or key" : "Paste a token or key";
  },
  get pasteTone() {
    return this.isSet || this.canSignIn ? "" : "primary";
  },
  get oauthChosen() {
    return this.chosen === "oauth";
  },
  get apiKeyChosen() {
    return this.chosen === "api_key";
  },
  get noPasteForm() {
    return !this.pasting;
  },
  get signingIn() {
    return ACTIVE.includes(this.phase);
  },
  get starting() {
    return this.phase === "starting";
  },
  get hasUrl() {
    return this.oauthUrl !== "";
  },
  get signInFailed() {
    return this.phase === "failed";
  },
  get hasDetail() {
    return this.oauthDetail !== "";
  },
  get noSignInHint() {
    return !this.canSignIn;
  },
  get signInDisabled() {
    return this.busy || this.signingIn || !this.canSignIn;
  },
  get pasteDisabled() {
    return this.busy || this.signingIn;
  },
  get codeDisabled() {
    return this.phase !== "waiting_for_code";
  },
  get signInLabel() {
    return "Sign in with Claude";
  },
  get signInTone() {
    return this.isSet ? "" : "primary";
  },
  get oauthStatusLine() {
    if (this.phase === "checking") return "Checking the code with Claude…";
    if (this.phase === "waiting_for_code") return `Waiting for the code · ${clock(this.remaining)} left`;
    return "";
  },

  async startSignIn() {
    if (this.signInDisabled) return;
    this.error = null;
    this.pasting = false;
    this.confirming = false;
    this.signInSaved = false;
    this.busy = true;
    try {
      this.applyOAuth(await gateway.oauthStart());
    } catch (err) {
      this.error = describeError(err);
      this.refreshSignIn();
    } finally {
      this.busy = false;
    }
  },
  /** @param {import("../../domain/ports.js").OAuthState} state */
  applyOAuth(state) {
    const was = this.phase;
    this.phase = state.phase;
    this.oauthUrl = state.url;
    this.oauthMessage = state.message;
    this.oauthDetail = state.detail;
    this.remaining = state.remaining_seconds;
    if (ACTIVE.includes(state.phase)) this.startTimer();
    else this.stopTimer();
    if (state.phase === "done" && ACTIVE.includes(was)) this.finishSignIn();
  },
  /** Claude is signed in: the panel closes on a confirmation. */
  async finishSignIn() {
    this.signInSaved = true;
    this.announcement = "Signed in with Claude.";
    try {
      this.apply(await gateway.get());
    } catch (err) {
      this.error = describeError(err);
    }
    await this.checkAuth();
    this.phase = "idle";
  },
  /** Asks `claude auth status`; a failure is "could not check". */
  async checkAuth() {
    this.authChecking = true;
    try {
      this.setAuth(await gateway.authStatus());
    } catch {
      this.setAuth({ signed_in: false, account: "", subscription: "", auth_method: "", checked_ok: false });
    } finally {
      this.authChecking = false;
    }
  },
  /** Runs `claude auth logout` on the server, then shows what the status says. */
  async signOut() {
    if (this.signOutDisabled) return;
    this.busy = true;
    this.error = null;
    try {
      this.setAuth(await gateway.signOut());
      this.announcement = "Signed out of Claude.";
      this.$nextTick(() => this.$refs.signin?.focus());
    } catch (err) {
      this.error = describeError(err);
      await this.checkAuth();
    } finally {
      this.busy = false;
    }
  },
  toggleSecondary() {
    if (this.secondaryOpen && this.pasting) this.cancelPaste();
    this.secondaryOpen = !this.secondaryOpen;
  },
  startTimer() {
    if (this.timer) return;
    this.timer = window.setInterval(() => this.tick(), 1000);
  },
  stopTimer() {
    window.clearInterval(this.timer);
    this.timer = 0;
  },
  tick() {
    this.remaining = Math.max(0, this.remaining - 1);
    this.ticks += 1;
    if (this.ticks % POLL_EVERY === 0) this.refreshSignIn();
  },
  async refreshSignIn() {
    try {
      this.applyOAuth(await gateway.oauthState());
    } catch {
      // The next tick asks again.
    }
  },
  /** Called with the code field in view: the panel's form submit. */
  async submitCode() {
    const field = /** @type {HTMLInputElement} */ (this.$refs.code);
    if (this.busy || !field) return;
    this.busy = true;
    this.error = null;
    try {
      this.applyOAuth(await gateway.oauthSubmit(field.value));
      field.value = "";
    } catch (err) {
      this.error = describeError(err);
    } finally {
      this.busy = false;
    }
  },
  async cancelSignIn() {
    this.error = null;
    try {
      this.applyOAuth(await gateway.oauthCancel());
      this.clearValue();
      this.$nextTick(() => this.$refs.signin?.focus());
    } catch (err) {
      this.error = describeError(err);
    }
  },
  async dismissSignIn() {
    await this.cancelSignIn();
  },
  /** The link opens in a new tab; step 3 is next, so the code field is ready. */
  opened() {
    this.$nextTick(() => this.$refs.code?.focus());
  },
  async copyLink() {
    try {
      await navigator.clipboard.writeText(this.oauthUrl);
      this.announcement = "Link copied.";
    } catch {
      this.announcement = "Could not copy the link. Select it and copy it by hand.";
    }
  },

  startPaste() {
    this.pasting = true;
    this.confirming = false;
    this.error = null;
    this.$nextTick(() => this.$refs.value?.focus());
  },
  cancelPaste() {
    this.clearValue();
    this.pasting = false;
    this.error = null;
    this.$nextTick(() => this.$refs.paste?.focus());
  },
  /** @param {Event} event the field's input */
  detectKind(event) {
    const kind = kindOf(/** @type {HTMLInputElement} */ (event.target).value);
    if (kind) this.chosen = kind;
  },
  chooseKind(event) {
    this.chosen = /** @type {HTMLInputElement} */ (event.target).value;
  },
  /** Called with the field in view: the form's submit. */
  async save() {
    if (this.busy) return;
    const field = /** @type {HTMLInputElement} */ (this.$refs.value);
    this.busy = true;
    this.error = null;
    try {
      this.apply(await gateway.set(/** @type {any} */ (this.chosen), field.value));
      this.clearValue();
      this.pasting = false;
      this.announcement = "Agent credential saved.";
      this.$nextTick(() => this.$refs.paste?.focus());
    } catch (err) {
      this.error = describeError(err);
    } finally {
      this.busy = false;
    }
  },
  clearValue() {
    for (const ref of [this.$refs.value, this.$refs.code]) {
      const field = /** @type {HTMLInputElement | undefined} */ (ref);
      if (field) field.value = "";
    }
  },
  /** The page is going away: the value must not outlive it in a restored page. */
  pagehide() {
    this.clearValue();
  },

  askRemove() {
    this.confirming = true;
    this.pasting = false;
    this.$nextTick(() => this.$refs.keep?.focus());
  },
  keep() {
    this.confirming = false;
    this.$nextTick(() => this.$refs.remove?.focus());
  },
  async remove() {
    if (this.busy) return;
    this.busy = true;
    this.error = null;
    this.confirming = false;
    try {
      this.apply(await gateway.remove());
      this.announcement = "Agent credential removed.";
    } catch (err) {
      this.error = describeError(err);
    } finally {
      this.busy = false;
    }
  },
  dismissError() {
    this.error = null;
  },
});
