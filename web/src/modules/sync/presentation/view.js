// How git state reads on screen: the chip's words, the fetch age, the panel's
// rows, the push question and what each refusal means. Plain functions over
// domain values; the components bind what they return. Copy follows the UX
// study's sections 3.1 to 3.3.

import { Codes, codeOf } from "../../../shared/domain/errors.js";
import { describeError } from "../../../shared/presentation/errors.js";
import { canPush, commitsToPush, isOld, pushTarget } from "../domain/gitState.js";

const plural = (n, one, many) => `${n} ${n === 1 ? one : many}`;

/** The block's data-state for each upstream state. */
const CHIP_STATE = Object.freeze({ in_sync: "in-sync", ahead: "ahead", behind: "behind", diverged: "diverged", none: "none", gone: "gone", unknown: "unknown" });

/** The word a chip carries when it has no counts. */
const WORDS = Object.freeze({ in_sync: "in sync", none: "not pushed", gone: "upstream deleted", unknown: "not fetched" });

const COUNTED = ["ahead", "behind", "diverged"];

// ── The fetch age ──────────────────────────────────────────────────────────

/**
 * How long ago origin was last fetched, in words, and whether that is old
 * (over an hour; never fetched counts as old). A branch this server pushed
 * while the repository was never fetched is known as of the push: `pushed`
 * then says so, `ago` is the push's age and the counts are old only when the
 * push is.
 * @returns {{ ago: string, never: boolean, pushed: boolean, old: boolean }}
 */
export function fetchAge(fetchedAt, now = new Date(), pushedAt = null) {
  if (!fetchedAt && !pushedAt) return { ago: "", never: true, pushed: false, old: true };
  const since = fetchedAt ?? pushedAt;
  const minutes = Math.max(0, Math.floor((now.getTime() - since.getTime()) / 60000));
  let ago;
  if (minutes < 1) ago = "just now";
  else if (minutes < 60) ago = `${minutes} min ago`;
  else if (minutes < 24 * 60) ago = `${Math.floor(minutes / 60)} h ago`;
  else ago = `${plural(Math.floor(minutes / (24 * 60)), "day", "days")} ago`;
  return { ago, never: false, pushed: !fetchedAt, old: isOld(since, now) };
}

/** The panel's age line: "Remote last fetched 12 min ago". */
export function freshLine(fetchedAt, now = new Date(), pushedAt = null) {
  const age = fetchAge(fetchedAt, now, pushedAt);
  if (age.pushed) return `Pushed ${age.ago} · never fetched`;
  return age.never ? "Remote not fetched yet" : `Remote last fetched ${age.ago}`;
}

/** The History head's tail: "fetched 12 min ago". */
export function fetchedPhrase(fetchedAt, now = new Date(), pushedAt = null) {
  const age = fetchAge(fetchedAt, now, pushedAt);
  if (age.pushed) return `pushed ${age.ago} · never fetched`;
  return age.never ? "not fetched yet" : `fetched ${age.ago}`;
}

// ── The chip ───────────────────────────────────────────────────────────────

/** The name the chip shows: the remote alone for origin/<branch>, the full ref otherwise; "" with no upstream. */
export function upstreamLabel(state) {
  const { ref, remote } = state.upstream;
  if (!ref) return "";
  return remote && ref === `${remote}/${state.branch}` ? remote : ref;
}

/**
 * The sync-state chip of a state.
 * @param {import("../domain/gitState.js").GitState} state
 * @param {{ now?: Date, busy?: "" | "fetching" | "pushing" }} [ctx]
 */
export function chipOf(state, { now = new Date(), busy = "" } = {}) {
  const up = state.upstream;
  const hasCounts = COUNTED.includes(up.state);
  const ahead = hasCounts ? (up.ahead ?? 0) : 0;
  const behind = hasCounts ? (up.behind ?? 0) : 0;
  const word = busy === "fetching" ? "Fetching…" : busy === "pushing" ? "Pushing…" : (WORDS[up.state] ?? "");
  const label = upstreamLabel(state);
  const age = fetchAge(state.fetchedAt, now, state.pushedAt);
  const what = up.ref ? `Upstream ${up.ref}` : "No upstream";
  const facts = hasCounts ? `${plural(ahead, "commit", "commits")} ahead, ${plural(behind, "commit", "commits")} behind` : (WORDS[up.state] ?? "");
  return {
    state: CHIP_STATE[up.state],
    upstream: label,
    hasUpstream: label !== "",
    title: up.ref ?? "no upstream",
    hasCounts,
    ahead,
    behind,
    word,
    hasWord: word !== "",
    loading: busy !== "",
    old: hasCounts && age.old,
    describe: `${what}: ${facts}${age.never ? "" : `, ${age.pushed ? "pushed" : "fetched"} ${age.ago}`}.`,
  };
}

// ── The panel ──────────────────────────────────────────────────────────────

/** The base comparison as a chip: counts against the branch it was cut from. */
function baseChip(state) {
  const { gone, ahead, behind } = state.base;
  if (gone) return { state: "gone", hasCounts: false, ahead: 0, behind: 0, word: "base gone", hasWord: true };
  const kind = ahead > 0 && behind > 0 ? "diverged" : ahead > 0 ? "ahead" : behind > 0 ? "behind" : "in-sync";
  const hasCounts = kind !== "in-sync";
  return { state: kind, hasCounts, ahead, behind, word: hasCounts ? "" : "in sync", hasWord: !hasCounts };
}

function upstreamHint(state) {
  const up = state.upstream;
  const ref = up.ref ?? "";
  switch (up.state) {
    case "in_sync":
      return `Same commits as ${ref}.`;
    case "ahead":
      return `${plural(up.ahead ?? 0, "commit", "commits")} not pushed yet.`;
    case "behind":
      return `The remote has ${plural(up.behind ?? 0, "commit", "commits")} this branch lacks. A push would be refused.`;
    case "diverged":
      return "Both sides have commits. A push would be refused.";
    case "none":
      return `Never pushed. Push sets origin/${state.branch} as the upstream.`;
    case "gone":
      return "The remote branch was deleted. Push creates it again.";
    default:
      return "Fetch to read the counts.";
  }
}

function baseHint(state) {
  const { ref, gone, ahead, behind } = state.base;
  if (gone) return `The base branch ${ref} no longer exists.`;
  return `${plural(ahead, "commit", "commits")} ahead of ${ref}, ${behind} behind.`;
}

/** "Push 3 commits", "Push 1 commit", "Push" when the count is not known. */
export function pushLabel(state) {
  const n = commitsToPush(state);
  return n === null ? "Push" : `Push ${plural(n, "commit", "commits")}`;
}

/** The question the confirm strip asks: names the remote branch and the commit count. */
export function pushQuestion(state) {
  const n = commitsToPush(state);
  const what = n === null ? "the branch" : plural(n, "commit", "commits");
  return `Push ${what} to ${pushTarget(state)}?`;
}

/**
 * The sync panel of a state: head, one row per comparison, the age line, the
 * push button and its question.
 * @param {import("../domain/gitState.js").GitState} state
 * @param {{ now?: Date, busy?: "" | "fetching" | "pushing" }} [ctx]
 */
export function panelOf(state, { now = new Date(), busy = "" } = {}) {
  const age = fetchAge(state.fetchedAt, now, state.pushedAt);
  const offered = canPush(state);
  return {
    branch: state.branch || "detached HEAD",
    commit: state.head.slice(0, 7),
    base: { prefix: "vs base", ref: state.base.ref, chip: baseChip(state), hint: baseHint(state) },
    upstream: { prefix: "vs upstream", ref: state.upstream.ref ?? "", chip: chipOf(state, { now, busy }), hint: upstreamHint(state) },
    fresh: { line: freshLine(state.fetchedAt, now, state.pushedAt), old: age.old },
    fetchLabel: busy === "fetching" ? "Fetching…" : "↻ Fetch",
    offersPush: offered,
    pushLabel: busy === "pushing" ? "Pushing…" : pushLabel(state),
    question: offered ? pushQuestion(state) : "",
    busy: busy !== "",
  };
}

/** "3 changed · 2 new"; "clean" when nothing is uncommitted. */
export function uncommittedLine({ tracked, untracked }) {
  const parts = [];
  if (tracked > 0) parts.push(`${tracked} changed`);
  if (untracked > 0) parts.push(`${untracked} new`);
  return parts.length ? parts.join(" · ") : "clean";
}

// ── Refusals ───────────────────────────────────────────────────────────────

/** The Git credentials page (the credentials module's; modules do not import each other). */
export const CREDENTIALS_HREF = "/git-credentials";

const credentialsLink = (params = {}) => {
  const q = new URLSearchParams(Object.entries(params).filter(([, v]) => v));
  return q.size ? `${CREDENTIALS_HREF}?${q}` : CREDENTIALS_HREF;
};

/** The first line of git's reason, trimmed. */
const firstLine = (reason) => reason.split("\n")[0].trim().replace(/[.]+$/, "");

/** The server's reason without the code its messages start with. */
export function reasonOf(err) {
  const message = err instanceof Error ? err.message : "";
  const code = codeOf(err);
  return message.startsWith(code) ? message.slice(code.length).replace(/^[:\s]+/, "") : message;
}

/**
 * A refused fetch or push as the panel shows it. A credential refusal is a
 * banner with the host and the link and nothing else (no reason, no code: git's
 * own words could carry a secret); every other refusal is a danger strip with
 * the code's words and git's reason.
 * @returns {{ tone: "banner" | "danger", lead: string, reason: string, code: string, href: string, linkText: string }}
 */
export function refusalOf(err) {
  const code = codeOf(err);
  const details = err?.details ?? {};
  const host = details.host || "the host";
  const credential = (lead, href, linkText) => ({ tone: "banner", lead, reason: "", code: "", href, linkText });
  const danger = (lead, reason = "") => ({ tone: "danger", lead, reason, code, href: "", linkText: "" });

  switch (code) {
    case Codes.GIT_CREDENTIAL_MISSING: {
      const what = details.kind === "ssh_key" ? "SSH key" : "credential";
      return credential(`No ${what} stored for ${host}.`, credentialsLink({ host: details.host, kind: details.kind }), "Add one in Git credentials");
    }
    case Codes.GIT_CREDENTIAL_REJECTED:
      return credential(`${host} refused the stored credential.`, details.credential_id ? credentialsLink({ credential: details.credential_id }) : credentialsLink({ host: details.host }), "Replace it in Git credentials");
    case Codes.GIT_CREDENTIAL_STORE_LOCKED:
      return credential("The server can't read its stored credentials.", CREDENTIALS_HREF, "See Git credentials");
    case Codes.PUSH_REJECTED:
      return danger(`The remote refused the push: ${firstLine(reasonOf(err))}. Nothing was pushed.`);
    case Codes.PUSH_STALE:
      return danger("The branch moved since you confirmed. Check the new commits and push again.");
    case Codes.PUSH_FAILED:
      return danger(`The push failed: ${firstLine(reasonOf(err))}.`);
    case Codes.FETCH_FAILED:
      return danger(`The fetch failed: ${firstLine(reasonOf(err))}. Counts are as of the last fetch.`);
    case Codes.NO_REMOTE:
      return danger("This repository has no origin remote.");
    case Codes.WORKSPACE_MISSING:
      return danger("The worktree is gone, so its branch cannot be pushed from here.");
    case Codes.UNAVAILABLE:
      return danger("Git actions are not available on this server.");
    default: {
      const d = describeError(err);
      return danger(d.message, d.next);
    }
  }
}
