// Symlinks are never followed by the server (contract §10 C3): the UI shows
// the target and, when it stays inside the source's root, offers to open it.
// The check is the UI's own, because a target outside the root answers
// INVALID_INPUT.

import { dirName } from "./code.js";

/**
 * The repository-relative path a link points at, or null when it leaves the
 * root (an absolute target, or one that climbs above it).
 * @param {string} linkPath  the link's own path
 * @param {string} target    as the tree lists it
 * @returns {string|null}
 */
export function resolveTarget(linkPath, target) {
  if (!target || target.startsWith("/") || target.includes("\0")) return null;
  const parts = dirName(linkPath) ? dirName(linkPath).split("/") : [];
  for (const segment of target.split("/")) {
    if (segment === "" || segment === ".") continue;
    if (segment === "..") {
      if (parts.length === 0) return null;
      parts.pop();
      continue;
    }
    parts.push(segment);
  }
  const path = parts.join("/");
  if (path === "" || path === ".git" || path.startsWith(".git/")) return null;
  return path;
}
