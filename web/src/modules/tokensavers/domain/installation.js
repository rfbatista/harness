// A token saver as the Token savers page shows it: its state in words, its
// versions, and what a person can do with it. The server paints the same
// words on first paint (tokensavers.Row in Go).

/**
 * @typedef {"not_installed"|"installed"|"outdated"|"installing"|"failed"} SaverState
 *
 * @typedef {object} Saver
 * @property {string} saver
 * @property {string} summary
 * @property {string} pinnedVersion
 * @property {SaverState} state
 * @property {string} installedVersion  "" when nothing is installed
 * @property {string} path              the installed executable or ruleset
 * @property {string} progress
 * @property {{ code: string, message: string } | null} error
 * @property {boolean} onSomewhere      a project, repository or task has it on
 *
 * @typedef {object} SaverList
 * @property {Saver[]} savers
 * @property {string} platform
 * @property {boolean} supported
 * @property {string} unsupported  why nothing installs, when not supported
 */

/** @param {Saver} s */
export function stateLabel(s) {
  switch (s.state) {
    case "installed":
      return "Installed";
    case "outdated":
      return "Update available";
    case "installing":
      return "Installing";
    case "failed":
      return "Install failed";
    default:
      return "Not installed";
  }
}

/** @param {Saver} s */
export function stateTone(s) {
  switch (s.state) {
    case "installed":
      return "signal";
    case "outdated":
      return "attention";
    case "failed":
      return "danger";
    default:
      return "";
  }
}

/** "Installed v1.0.1", "v1.0.0 installed · pins v1.0.1" or "Pins v1.0.1". @param {Saver} s */
export function versionsText(s) {
  if (!s.installedVersion) return `Pins ${s.pinnedVersion}`;
  if (s.installedVersion === s.pinnedVersion) return `Installed ${s.installedVersion}`;
  return `${s.installedVersion} installed · pins ${s.pinnedVersion}`;
}

/** Install, or Update over another version; "" while it installs or once it is current. @param {Saver} s */
export function installLabel(s) {
  if (s.state === "installing") return "";
  if (s.installedVersion === s.pinnedVersion) return "";
  return s.installedVersion ? "Update" : "Install";
}

/** @param {Saver} s */
export function canRemove(s) {
  return s.state !== "installing" && s.installedVersion !== "";
}

/** Why sessions run without it: on somewhere but not installed; "" otherwise. @param {Saver} s */
export function notInstalledNotice(s) {
  return s.onSomewhere && !s.installedVersion ? NOT_INSTALLED_NOTICE : "";
}

/** The server paints the same words (tokensavers.NotInstalledNotice in Go). */
export const NOT_INSTALLED_NOTICE = "Turned on for some sessions but not installed, so they run without it.";
