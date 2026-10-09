import { MAX_TITLE } from "./resources.js";

// The by-id lists of a task's resources and comments, and the echo guard
// that keeps the person's own additions from showing twice.
//
// The project feed echoes what the person adds, possibly before the POST
// that added it answers. An upload has no id until that answer, so an id
// cannot match it: each pending add makes a claim (what it sent), and an
// unknown item off the feed that fits an open claim is held until the claim
// settles. The answer's own echo is then dropped; anything else held is
// shown, because it was a twin added elsewhere.

/** The list with the item in its place, or added at the end (the lists are oldest first). */
export function upsert(list, item) {
  const i = list.findIndex((x) => x.id === item.id);
  if (i < 0) return [...list, item];
  const next = [...list];
  next[i] = item;
  return next;
}

export const removeById = (list, id) => list.filter((x) => x.id !== id);

export const hasId = (list, id) => list.some((x) => x.id === id);

// What a pending add sent, and what an item off the feed looks like, in the
// same words. The server trims titles, URLs and bodies; so do the claims.

/** @param {import("./resources.js").Resource} r */
export const resourceSignature = (r) => (r.kind === "link" ? `link\n${r.url}` : `upload\n${r.title}\n${r.sizeBytes}`);
export const uploadClaim = (title, size) => `upload\n${title.trim()}\n${size}`;

/**
 * The title the server gives an upload sent without one (domain.DefaultResourceTitle):
 * the file's base name, trimmed, cut to 200 runes.
 */
export function defaultUploadTitle(fileName) {
  const base = (fileName ?? "").split(/[\\/]/).at(-1) ?? "";
  return [...base.trim()].slice(0, MAX_TITLE).join("");
}
export const linkClaim = (url) => `link\n${url.trim()}`;

/** @param {import("./comments.js").Comment} c */
export const commentSignature = (c) => c.body;
export const commentClaim = (body) => body.trim();

/**
 * @template {{ id: string }} T
 * @param {(item: T) => string} signature
 */
export function echoGuard(signature) {
  let nextClaim = 1;
  /** @type {Map<number, { sig: string, heldId: string }>} */
  const claims = new Map();
  /** @type {Map<string, { item: T, claimId: number }>} */
  const held = new Map();

  return {
    /** A pending add: returns the claim to settle when it answers. */
    claim(sig) {
      const id = nextClaim++;
      claims.set(id, { sig, heldId: "" });
      return id;
    },

    /**
     * An item the list does not have came off the feed. True when it is held
     * for a pending add (and must not be shown yet); false to show it now.
     * @param {T} item
     */
    hold(item) {
      const earlier = held.get(item.id);
      if (earlier) {
        earlier.item = item; // a retitle or edit of a held item keeps it held, up to date
        return true;
      }
      const sig = signature(item);
      for (const [claimId, c] of claims) {
        if (c.heldId === "" && c.sig === sig) {
          c.heldId = item.id;
          held.set(item.id, { item, claimId });
          return true;
        }
      }
      return false;
    },

    /** A delete came off the feed: a held item is forgotten. True when it was held. */
    drop(id) {
      const h = held.get(id);
      if (!h) return false;
      held.delete(id);
      const c = claims.get(h.claimId);
      if (c) c.heldId = "";
      return true;
    },

    /**
     * The add answered with the item it made, or failed ("" ). Returns the
     * held items to show now: everything held but the answer's own echo.
     * @returns {T[]}
     */
    settle(claimId, answeredId = "") {
      const c = claims.get(claimId);
      claims.delete(claimId);
      if (!c || c.heldId === "") return [];
      const h = held.get(c.heldId);
      held.delete(c.heldId);
      return h && h.item.id !== answeredId ? [h.item] : [];
    },

    /** Adds still waiting on their answer. */
    get pending() {
      return claims.size;
    },
  };
}
