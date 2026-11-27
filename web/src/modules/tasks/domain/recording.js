// A task's trajectory recording (ADR 0015) in the shape of its Edit form:
// inherit / on / off, with the line that says what holds and where it comes
// from. null from the server is inherit.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";

/**
 * @typedef {"inherit"|"on"|"off"} RecordingChoice
 *
 * @typedef {object} Recording
 * @property {string} taskId
 * @property {boolean} enabled          whether the task's sessions are recorded
 * @property {boolean|null} explicit    what the task sets itself; null while it inherits
 * @property {boolean} default          the settings default the task follows when it sets nothing
 * @property {string} defaultSource     "repository", "project" or "harness"; "" when unknown
 */

export const RECORDING_CHOICES = /** @type {{ value: RecordingChoice, label: string }[]} */ ([
  { value: "inherit", label: "Inherit" },
  { value: "on", label: "On" },
  { value: "off", label: "Off" },
]);

/** @param {boolean|null} explicit */
export const choiceOf = (explicit) => (explicit === null ? "inherit" : explicit ? "on" : "off");

/** @param {RecordingChoice} choice  the flag the server stores: null follows the default */
export const enabledOf = (choice) => (choice === "inherit" ? null : choice === "on");

const word = (on) => (on ? "on" : "off");

/**
 * What holds if the task has `choice`: "on, from this task; project default off".
 * @param {Recording} rec
 * @param {RecordingChoice} choice
 */
export function resolution(rec, choice) {
  const level = rec.defaultSource;
  if (choice === "inherit") return level ? `${word(rec.default)}, from the ${level} default` : word(rec.default);
  const own = `${word(choice === "on")}, from this task`;
  return level ? `${own}; ${level} default ${word(rec.default)}` : own;
}

/** @param {any} dto  the trajectory-recording answer */
export function decodeRecording(dto) {
  const bad = (detail) => {
    throw new StructuredError(Codes.BAD_RESPONSE, `Unexpected recording data: ${detail}.`);
  };
  if (!dto || typeof dto !== "object") return bad("expected an object");
  if (typeof dto.task_id !== "string") bad("no task_id");
  if (dto.explicit !== null && typeof dto.explicit !== "boolean") bad("explicit is neither a boolean nor null");
  return {
    taskId: dto.task_id,
    enabled: dto.enabled === true,
    explicit: dto.explicit,
    default: dto.default === true,
    defaultSource: typeof dto.default_source === "string" ? dto.default_source : "",
  };
}
