// The upload queue: each file the person adds becomes a row at once, waits
// for one of three slots, uploads with progress, and either becomes the
// resource the server made or stays as a failed row with what to do next.
// Rows here are plain values; the page keeps the list and starts the uploads.

import { Codes } from "../../../shared/domain/errors.js";
import { sizeWords, tooLarge } from "./resources.js";

/** Uploads that run at once; the rest wait. */
export const MAX_PARALLEL = 3;

export const UploadState = Object.freeze({ WAITING: "waiting", UPLOADING: "uploading", FAILED: "failed" });

/**
 * @typedef {object} Failure
 * @property {string} message  what happened, in words
 * @property {string} code     the error code, or "network"
 * @property {string} next     what to do about it
 * @property {boolean} retry   trying again may work
 *
 * @typedef {object} UploadRow
 * @property {string} key      the row's own id until the server gives one
 * @property {string} name     the file's name
 * @property {number} size
 * @property {string} title    the title sent with it ("" lets the server use the name)
 * @property {"waiting"|"uploading"|"failed"} state
 * @property {number} loaded   bytes sent so far
 * @property {Failure|null} failure
 */

/**
 * A row for a file the person added. A file the server would refuse fails
 * here, before a byte is sent.
 * @param {string} key
 * @param {{ name: string, size: number }} file
 * @param {string} [title]
 * @returns {UploadRow}
 */
export function newUpload(key, file, title = "") {
  const row = { key, name: file.name, size: file.size, title, state: UploadState.WAITING, loaded: 0, failure: null };
  if (tooLarge(file.size)) return failed(row, Codes.RESOURCE_TOO_LARGE);
  if (file.size === 0) return failed(row, Codes.INVALID_INPUT);
  return row;
}

/** The waiting rows that may start now, in the order they were added. */
export function startable(rows) {
  const running = rows.filter((r) => r.state === UploadState.UPLOADING).length;
  return rows.filter((r) => r.state === UploadState.WAITING).slice(0, Math.max(0, MAX_PARALLEL - running));
}

export const started = (row) => ({ ...row, state: UploadState.UPLOADING, loaded: 0, failure: null });
export const progressed = (row, loaded) => ({ ...row, loaded: Math.min(loaded, row.size) });
export const failed = (row, code, message = "") => ({ ...row, state: UploadState.FAILED, failure: failureFor(code, row.size, message) });
/** Retry puts a failed row back in the queue. */
export const retried = (row) => ({ ...row, state: UploadState.WAITING, loaded: 0, failure: null });

/** What the row shows: its title, as the resource will be called. */
export const uploadTitle = (row) => row.title || row.name;

export const percent = (row) => (row.size > 0 ? Math.floor((row.loaded / row.size) * 100) : 0);

/** "Uploading · 14.5 MB of 23.9 MB · 61%", "Waiting · 8.2 MB". A failed row shows its failure instead. */
export function progressWords(row) {
  if (row.state === UploadState.UPLOADING) return `Uploading · ${sizeWords(row.loaded)} of ${sizeWords(row.size)} · ${percent(row)}%`;
  if (row.state === UploadState.WAITING) return `Waiting · ${sizeWords(row.size)}`;
  return "";
}

/**
 * The words for a failed upload, by the code it failed with.
 * @returns {Failure}
 */
export function failureFor(code, size, message = "") {
  switch (code) {
    case Codes.RESOURCE_TOO_LARGE:
      return { message: `${sizeWords(size)} is over the 25 MiB limit.`, code, next: "compress it or add a link", retry: false };
    case Codes.INVALID_INPUT:
      return { message: size === 0 ? "This file is empty." : message || "The server refused this file.", code, next: "choose another file", retry: false };
    case Codes.TICKET_NOT_FOUND:
      return { message: "This task no longer exists.", code, next: "back to the project", retry: false };
    case Codes.NETWORK:
      return { message: "The upload stopped: the connection dropped.", code: "network", next: "try again", retry: true };
    default:
      return { message: message || "The upload stopped: the server could not store it.", code: code || Codes.UNKNOWN, next: "try again", retry: true };
  }
}
