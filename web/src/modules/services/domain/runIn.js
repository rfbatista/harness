// The "Run in" pick of a command service that has a repository (ADR 0077): the
// run worktrees on offer, which one is picked, and what happens when a worktree
// goes. Pure state transitions, so the page only holds the result. The words of
// each option are the App tab's (shared/domain/runWorktree.js).
//
// A pick is `{ worktrees, state, pickedKey, restored, fallback }`:
//   state     "loading" until the first list arrives; "failed" keeps Start working in the main checkout
//   restored  whether the remembered pick has been looked at (it waits for the first list that arrives)
//   fallback  {message} when the remembered or current pick is gone; null for none

import { agoLabel } from "./service.js";
import { branchLabel, worktreeDescription, worktreeKindWord, worktreeTone } from "../../../shared/domain/runWorktree.js";

/** The value of the "Branch…" entry of the picker: it opens the branch search, it is not a pick. */
export const BRANCH_ENTRY = "branch:";

/** Whether a key names a branch (`branch:<name>`, `branch:origin/<name>` for a remote one). */
export const isBranchKey = (key) => key.startsWith("branch:") && key.length > BRANCH_ENTRY.length;

/** A branch key's name as shown: the key without its prefix. */
export const branchNameOf = (key) => key.slice(BRANCH_ENTRY.length);

/** The key of the main checkout: the default pick, which the server also reads from an empty key. */
export const MAIN = "main";

/** Where a service's pick is kept in the browser (ADR 0037): per service, apart from the App tab's key. */
export const pickKeyOf = (serviceId) => `service-run-worktree:${serviceId}`;

/** @param {{ kind: string, repositoryId: string }} service */
export const hasRunIn = (service) => service.kind === "command" && service.repositoryId !== "";

export const freshRunIn = () => ({ worktrees: [], state: "loading", pickedKey: MAIN, restored: false, fallback: null });

// A branch is kept as picked: no list says it is still there, the server refuses a start when it is gone.
const offered = (pick, key) => isBranchKey(key) || pick.worktrees.some((w) => w.key === key);

/** The worktree that is picked, or null before a list has it. */
export const pickedWorktree = (pick) => pick.worktrees.find((w) => w.key === pick.pickedKey) ?? null;

/** What a start sends: nothing until a list is in, which the server reads as the main checkout. */
export const startKey = (pick) => (pick.state === "ready" || isBranchKey(pick.pickedKey) ? pick.pickedKey : "");

const fallback = (gone) => {
  const what = gone?.kind === "session" && gone.agent ? `The worktree of ${gone.agent}${gone.branch ? ` on ${gone.branch}` : ""}` : "That worktree";
  return { message: `${what} no longer exists. Running in the main checkout.` };
};

/**
 * A list arrived. The first one restores the remembered pick (`stored`); a later one drops a pick
 * that is no longer on offer. `silent` says no notice (the refusal already shows one).
 * @returns {{ next: object, clearStored: boolean }}
 */
export function afterRead(prev, worktrees, { stored, silent = false }) {
  const read = { ...prev, worktrees, state: "ready", restored: true };
  if (!prev.restored) {
    if (stored === null || stored === MAIN) return { next: read, clearStored: false };
    if (offered(read, stored)) return { next: { ...read, pickedKey: stored }, clearStored: false };
    return { next: { ...read, pickedKey: MAIN, fallback: silent ? null : fallback(null) }, clearStored: true };
  }
  if (offered(read, prev.pickedKey)) return { next: read, clearStored: false };
  return { next: { ...read, pickedKey: MAIN, fallback: silent ? null : fallback(pickedWorktree(prev)) }, clearStored: true };
}

/** A list that cannot be read: one already read stays good enough, so the pick never silently turns into the main checkout. */
export function afterFailedRead(prev) {
  return prev.restored ? prev : { ...prev, state: "failed" };
}

/**
 * A start was refused as WORKTREE_NOT_AVAILABLE. A branch that is gone is forgotten and the pick goes back
 * to the main checkout, with no notice of its own (the refusal says it). A worktree is left to the re-read.
 */
export function afterRefusal(prev) {
  if (!isBranchKey(prev.pickedKey)) return { next: prev, clearStored: false };
  return { next: { ...prev, pickedKey: MAIN, fallback: null }, clearStored: true };
}

/** What Restart does for a run, by its run worktree key (the run's `worktree_key`). */
export const restartNote = (worktreeKey) => (isBranchKey(worktreeKey)
  ? "Runs the same branch again at its latest commit"
  : "Repeats the run: the same worktree and the same overrides");

/** A worktree is picked; null when it is not on offer. `clearStored` is the main checkout, the default, which is not remembered. */
export function afterPick(prev, key) {
  if (!offered(prev, key)) return null;
  return { next: { ...prev, pickedKey: key, fallback: null }, clearStored: key === MAIN };
}

/**
 * The picker's options: the main checkout, then the sessions' worktrees, in the server's order, then
 * the picked branch (which no list offers) and the "Branch…" entry that opens the search.
 */
export function runInOptions(pick) {
  const option = (w) => ({ key: w.key, label: branchLabel(w.branch), description: worktreeDescription(w), tone: worktreeTone(w) });
  const picked = isBranchKey(pick.pickedKey) ? pick.pickedKey : null;
  return {
    main: pick.worktrees.filter((w) => w.kind === "main").map(option),
    sessions: pick.worktrees.filter((w) => w.kind === "session").map(option),
    branch: picked && {
      key: picked,
      label: branchNameOf(picked),
      description: branchNameOf(picked).startsWith("origin/") ? "Remote branch · fetched when you start" : "Branch · committed work only",
    },
    branchEntry: { key: BRANCH_ENTRY, label: "Branch…", description: "Any local or origin branch" },
  };
}

const NO_MORE = "Showing the first 200, type to narrow";

/**
 * The branch search's options (a list_service_branches answer): local branches, then remote ones,
 * which a start fetches first. `truncated` adds a line that is not an option.
 */
export function branchOptions(list, now) {
  const options = list.branches.map((b) => ({
    value: `${BRANCH_ENTRY}${b.remote ? "origin/" : ""}${b.name}`,
    label: `${b.remote ? "origin/" : ""}${b.name}`,
    group: b.remote ? "Remote branches · fetched first" : "Local branches",
    description: [b.sha, b.subject].filter(Boolean).join(" · "),
    meta: b.committedAt ? agoLabel(b.committedAt, now) : "",
  }));
  return list.truncated ? [...options, { value: "", label: NO_MORE, disabled: true }] : options;
}

/** A pick's notice is dismissed. */
export const afterDismiss = (prev) => ({ ...prev, fallback: null });

/** What a screen reader hears after a pick: "api runs in go-developer, branch agent/other." or "api runs branch feature/x." */
export function pickAnnouncement(serviceName, pick) {
  if (isBranchKey(pick.pickedKey)) return `${serviceName} runs branch ${branchNameOf(pick.pickedKey)}.`;
  const w = pickedWorktree(pick);
  return `${serviceName} runs in ${worktreeKindWord(w)}, branch ${branchLabel(w.branch)}.`;
}
