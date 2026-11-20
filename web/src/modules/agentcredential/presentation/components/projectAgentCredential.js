// A project's own agent credential, in its settings page: what its sessions
// sign in with instead of the global Agent credential. Without one they fall
// back to the global credential, or to a Claude login in the agents' home.
//
//   <section x-data="projectAgentCredential" data-seed="project-credential-seed">
//
// The seed is {project_id, set, kind, updated, account, subscription,
// effective, global: {set, kind}, oauth, can_sign_in}: kind is "login" for a
// project login (ADR 0102), and effective is the credential the project's
// next claude process runs with. The value
// is typed into an uncontrolled password input, sent once, and cleared on save,
// cancel and pagehide; it is never held in component state and never shown
// again.
//
// Sign in with Claude runs `claude auth login` on the server, in the project's
// own Claude home (ADR 0102); the project's flow lives there, so the seed
// carries it and a page that was left finds the same panel. Claude keeps the
// login; the pasted code is not held here, and who the project is signed in as
// is `claude auth status`, read when the page loads and after a sign-in.

import { describeError } from "../../../../shared/presentation/errors.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { clock, kindOf } from "./agentCredential.js";

const KIND_NAMES = { oauth: "OAuth token", api_key: "Anthropic API key", login: "Claude login" };
const ACTIVE = ["starting", "waiting_for_code", "checking"];
/** How often the page asks the server how the flow is going, in ticks of a second. */
const POLL_EVERY = 2;

/** @param {{ gateway: import("../../domain/ports.js").AgentCredentialGateway }} deps */
export const projectAgentCredential = ({ gateway }) => () => ({
  projectId: "",
  isSet: false,
  kind: "",
  updated: "",
  /** The global credential is set, so a project without its own still has one. */
  globalSet: false,
  /** @type {import("../../domain/ports.js").EffectiveCredential} */
  effective: { source: "none", kind: "", account: "", subscription: "" },
  /** What the dialog asks to replace: "signin", "paste", "signout", or "". */
  replacing: "",
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
  /** `claude auth status` of the project Claude home. */
  signedIn: false,
  account: "",
  subscription: "",

  init() {
    const seed = readSeed(this.$el);
    this.projectId = seed.project_id;
    this.globalSet = seed.global?.set === true;
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
    this.signedIn = status.kind === "login";
    this.account = status.account ?? "";
    this.subscription = status.subscription ?? "";
    this.effective = status.effective ?? this.effective;
  },

  get oauthChosen() {
    return this.chosen === "oauth";
  },
  get apiKeyChosen() {
    return this.chosen === "api_key";
  },
  get notSet() {
    return !this.isSet;
  },
  /** A pasted credential, which Remove takes away; a login is signed out. */
  get isPasted() {
    return this.isSet && !this.signedIn;
  },
  get signInShown() {
    return !this.signedIn;
  },
  get chipText() {
    if (this.signedIn) return "Signed in";
    return this.isSet ? "Own credential" : "Global";
  },
  get chipTone() {
    return this.isSet || this.signedIn ? "signal" : "";
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
  get oauthStatusLine() {
    if (this.phase === "checking") return "Checking the code with Claude…";
    if (this.phase === "waiting_for_code") return `Waiting for the code · ${clock(this.remaining)} left`;
    return "";
  },
  get kindName() {
    return KIND_NAMES[this.kind] ?? "";
  },
  get savedDate() {
    return this.updated ? this.updated.slice(0, 10) : "";
  },
  get pasteLabel() {
    return this.isPasted ? "Replace with a pasted token or key" : "Paste a token or key";
  },
  get pasteTone() {
    return this.isSet ? "" : "primary";
  },
  /** What a session without a project credential runs with. */
  get fallbackLine() {
    const e = this.effective;
    if (e.source === "global_login") return e.account ? `Uses the global login: ${e.account}, ${e.subscription}` : "Uses the global login.";
    if (e.source === "global_pasted") return `Uses the global pasted ${e.kind === "api_key" ? "API key" : "OAuth token"}.`;
    return "No credential: agents cannot start.";
  },
  get replaceTitle() {
    return {
      signin: "Replace the pasted credential with a login?",
      paste: "Replace the login with a pasted credential?",
      signout: "Sign this project out of Claude?",
    }[this.replacing] ?? "";
  },
  get replaceText() {
    return {
      signin: "Signing in replaces the pasted credential: it is removed once the sign-in is done. A project holds one credential. Running sessions are not stopped.",
      paste: "Pasting a credential signs this project out of Claude: a project holds one credential. Running sessions lose access when claude next refreshes.",
      signout: "Running sessions lose access when claude next refreshes. Sessions started after this use the global credential again.",
    }[this.replacing] ?? "";
  },
  get replaceConfirm() {
    return { signin: "Sign in and replace", paste: "Replace login", signout: "Sign out of Claude" }[this.replacing] ?? "";
  },

  /** Signing in over a pasted credential asks first. */
  askSignIn() {
    if (this.signInDisabled) return;
    if (this.isPasted) this.ask("signin");
    else this.startSignIn();
  },
  /** Pasting over a login asks first. */
  askPaste() {
    if (this.signedIn) this.ask("paste");
    else this.startPaste();
  },
  askSignOut() {
    if (!this.pasteDisabled) this.ask("signout");
  },
  /** @param {"signin" | "paste" | "signout"} what */
  ask(what) {
    this.replacing = what;
    this.confirming = false;
    this.pasting = false;
    this.error = null;
    this.$nextTick(() => this.$refs.keepreplace?.focus());
  },
  keepReplacing() {
    this.replacing = "";
  },
  async confirmReplacing() {
    const what = this.replacing;
    this.replacing = "";
    if (what === "signin") await this.startSignIn();
    else if (what === "paste") this.startPaste();
    else if (what === "signout") await this.signOut();
  },
  /** Runs `claude auth logout` in the project Claude home, then reads the status again. */
  async signOut() {
    if (this.busy) return;
    this.busy = true;
    this.error = null;
    try {
      await gateway.projectSignOut(this.projectId);
      this.apply(await gateway.getForProject(this.projectId));
      this.announcement = "Signed out of Claude. Running sessions lose access when claude next refreshes.";
    } catch (err) {
      this.error = describeError(err);
    } finally {
      this.busy = false;
    }
  },
  async startSignIn() {
    if (this.signInDisabled) return;
    this.error = null;
    this.pasting = false;
    this.confirming = false;
    this.signInSaved = false;
    this.busy = true;
    try {
      this.applyOAuth(await gateway.projectOAuthStart(this.projectId));
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
  /** Claude is signed in: the panel closes on a confirmation and shows the account. */
  async finishSignIn() {
    this.signInSaved = true;
    this.announcement = "Signed in with Claude.";
    try {
      this.apply(await gateway.getForProject(this.projectId));
    } catch (err) {
      this.error = describeError(err);
    }
    this.phase = "idle";
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
      this.applyOAuth(await gateway.projectOAuthState(this.projectId));
    } catch {
      // The next tick asks again.
    }
  },
  /** The panel's form submit. */
  async submitCode() {
    const field = /** @type {HTMLInputElement} */ (this.$refs.code);
    if (this.busy || !field) return;
    this.busy = true;
    this.error = null;
    try {
      this.applyOAuth(await gateway.projectOAuthSubmit(this.projectId, field.value));
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
      this.applyOAuth(await gateway.projectOAuthCancel(this.projectId));
      this.clearValue();
      this.$nextTick(() => this.$refs.signin?.focus());
    } catch (err) {
      this.error = describeError(err);
    }
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
  /** The form's submit. */
  async save() {
    if (this.busy) return;
    const field = /** @type {HTMLInputElement} */ (this.$refs.value);
    this.busy = true;
    this.error = null;
    try {
      this.apply(await gateway.setForProject(this.projectId, /** @type {any} */ (this.chosen), field.value));
      this.clearValue();
      this.pasting = false;
      this.announcement = "Project agent credential saved.";
      this.$nextTick(() => this.$refs.paste?.focus());
    } catch (err) {
      this.error = describeError(err);
      // The save may have signed the project out first: show what is true now.
      await this.reread();
    } finally {
      this.busy = false;
    }
  },
  /** Reads the project's credential again, as it is on the server now. */
  async reread() {
    try {
      this.apply(await gateway.getForProject(this.projectId));
    } catch {
      // The error already shown stands.
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
    this.replacing = "";
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
      this.apply(await gateway.removeForProject(this.projectId));
      this.announcement = "Project agent credential removed. Sessions use the global credential again.";
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
