// How git credentials read on the page. The Go BFF paints the first rows
// with the same rules (internal/adapter/in/web/credentials/view.go).

import { relativeTime } from "../../../shared/presentation/format.js";
import { kindLabel, remoteOf } from "../domain/credential.js";

/** The Git credentials page. */
export const CREDENTIALS_HREF = "/git-credentials";

/** The page with its add form open and prefilled for a host (and a kind). */
export function addHref(host, kind = "") {
  const q = new URLSearchParams({ host });
  if (kind) q.set("kind", kind);
  return `${CREDENTIALS_HREF}?${q}`;
}

/** The page scrolled to one credential. */
export const credentialHref = (id) => `${CREDENTIALS_HREF}?${new URLSearchParams({ credential: id })}`;

/** "last used 3d ago", "never used". */
export function lastUsedLabel(lastUsedAt, now) {
  if (!lastUsedAt) return "never used";
  const age = relativeTime(lastUsedAt, now);
  return age === "now" ? "last used just now" : `last used ${age} ago`;
}

/**
 * One credential as its row shows it.
 * @param {import("../domain/credential.js").Credential} c
 * @param {Date} now
 */
export function credentialRow(c, now) {
  return {
    id: c.id,
    name: c.name,
    kind: c.kind,
    kindLabel: kindLabel(c.kind),
    isDefault: c.isDefault,
    meta: [c.username, c.fingerprint, lastUsedLabel(c.lastUsedAt, now)].filter(Boolean).join(" · "),
    publicKey: c.publicKey ?? "",
    hasPublicKey: !!c.publicKey,
    menuLabel: `Actions for ${c.name}`,
    testLabel: `Test ${c.name}`,
  };
}

/** A test's answer as the row's banner says it. */
export function testLine(result) {
  const message = result.message || (result.ok ? "Signed in." : "The host refused it.");
  return result.checkedUrl ? `${message} (${result.checkedUrl})` : message;
}

/**
 * What a repository's pushes use, in words, and what to do when nothing fits.
 * @param {import("../domain/credential.js").RepositoryCredential} rc
 * @returns {{ state: "pinned"|"automatic"|"none"|"local", text: string, name: string, addHref: string }}
 */
export function bindingLine(rc) {
  const remote = remoteOf(rc.remoteUrl);
  if (rc.credentialId && rc.resolved) return { state: "pinned", name: rc.resolved.name, text: "pinned", addHref: "" };
  if (!rc.remoteUrl) return { state: "local", name: "", text: "Local only: no remote, no credential needed.", addHref: "" };
  if (rc.resolved) {
    const why = rc.resolved.isDefault ? `default for ${rc.resolved.host}` : `the only ${kindLabel(rc.resolved.kind)} for ${rc.resolved.host}`;
    return { state: "automatic", name: rc.resolved.name, text: `automatic (${why})`, addHref: "" };
  }
  if (!remote) return { state: "none", name: "", text: "The origin is not an https or ssh URL, so no credential applies.", addHref: "" };
  const what = remote.kind === "ssh_key" ? "SSH key" : "token";
  return { state: "none", name: "", text: `No ${what} for ${remote.host}: pushes and pull requests from this repository will fail.`, addHref: addHref(remote.host, remote.kind) };
}
