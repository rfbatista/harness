// What each error of the code browsing contract (§7) reads as, and the next
// step it offers (UX §5). Branches on the code, never on the message.

/**
 * @typedef {object} CodeProblem
 * @property {string} code
 * @property {"banner"|"empty"} as      a danger banner in the main side, or an empty state in its place
 * @property {string} title             the empty state's heading, or "" for a banner
 * @property {string} text              what happened
 * @property {string} action            the next step's label ("" for none)
 * @property {"root"|"ref"|"branch"|"files"|""} next   what the action does
 */

/**
 * @param {string} code
 * @param {{ path?: string, ref?: string, repository?: string, defaultRef?: boolean }} [about]
 *   path / ref: what was asked; repository: its name; defaultRef: no ref was asked (the checkout's branch)
 * @returns {CodeProblem}
 */
export function problemOf(code, { path = "", ref = "", repository = "", defaultRef = false } = {}) {
  const on = ref ? ` on ${ref}` : "";
  switch (code) {
    case "PATH_NOT_FOUND":
      return { code, as: "banner", title: "", text: `${path || "That path"} is not${on || " here"}`, action: "Show the tree root", next: "root" };
    case "REF_NOT_FOUND":
      if (defaultRef) {
        return { code, as: "empty", title: "No commits yet", text: `${repository || "This repository"} has no commits to read. Files appear here after the first commit.`, action: "", next: "" };
      }
      return { code, as: "banner", title: "", text: `${ref || "That ref"} is not a branch or tag here`, action: "Pick another ref", next: "ref" };
    case "INVALID_INPUT":
      return { code, as: "banner", title: "", text: "Use a branch, tag or commit hash", action: "Pick another ref", next: "ref" };
    case "COMMIT_NOT_FOUND":
      return { code, as: "banner", title: "", text: `${ref || "That commit"} is not in this repository`, action: "Pick another ref", next: "ref" };
    case "UNAVAILABLE":
      return { code, as: "empty", title: "Code browsing is off on this server", text: "The server was started without the code browser. The rest of the page keeps working.", action: "", next: "" };
    case "REPOSITORY_NOT_FOUND":
      return { code, as: "empty", title: "Repository not found", text: "It was removed from this project.", action: "", next: "" };
    case "WORKSPACE_NOT_FOUND":
      return { code, as: "empty", title: "Worktree not found", text: "This session has no worktree on record.", action: "", next: "" };
    default:
      return { code, as: "banner", title: "", text: "Could not read this code", action: "", next: "" };
  }
}

/** The danger banner's line: "PATH_NOT_FOUND · internal/x.go is not on main". */
export const bannerLine = (p) => `${p.code} · ${p.text}`;
