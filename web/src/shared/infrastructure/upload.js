// Multipart POSTs with upload progress. fetch cannot report how much of a
// request body has been sent, so this one uses XMLHttpRequest, and answers
// the way api.js does: the JSON body, or a StructuredError with the server's
// code. A cancel rejects with an AbortError, as fetch does.

import { Codes, StructuredError } from "../domain/errors.js";

/**
 * Parts go into the form in the order given: a server that streams the body
 * may need the small fields before the file.
 * @typedef {[name: string, value: string | Blob, filename?: string][]} Parts
 *
 * @typedef {(path: string, parts: Parts, options?: {
 *   onProgress?: (loaded: number, total: number) => void,
 *   signal?: AbortSignal,
 * }) => Promise<any>} Uploader
 */

const abortError = () => new DOMException("The upload was cancelled.", "AbortError");

/**
 * @param {{ base: string, XMLHttpRequest?: typeof globalThis.XMLHttpRequest }} options
 * @returns {Uploader}
 */
export function xhrUpload({ base, XMLHttpRequest = globalThis.XMLHttpRequest }) {
  return (path, parts, { onProgress = () => {}, signal } = {}) =>
    new Promise((resolve, reject) => {
      if (signal?.aborted) {
        reject(abortError());
        return;
      }
      const form = new FormData();
      for (const [name, value, filename] of parts) {
        if (filename === undefined) form.append(name, value);
        else form.append(name, value, filename);
      }

      const xhr = new XMLHttpRequest();
      const onAbort = () => xhr.abort();
      const done = () => signal?.removeEventListener("abort", onAbort);

      xhr.upload.onprogress = (e) => {
        if (e.lengthComputable) onProgress(e.loaded, e.total);
      };
      xhr.onload = () => {
        done();
        let body = null;
        const text = xhr.responseText;
        if (text) {
          try {
            body = JSON.parse(text);
          } catch {
            // A proxy or the server's own size cut-off may answer without our JSON.
            if (xhr.status === 413) reject(new StructuredError(Codes.RESOURCE_TOO_LARGE, "The file is over the size limit.", 413));
            else reject(new StructuredError(Codes.BAD_RESPONSE, `Unreadable response from ${path}.`, xhr.status));
            return;
          }
        }
        if (xhr.status >= 200 && xhr.status < 300) resolve(body);
        else reject(new StructuredError(body?.code ?? (xhr.status === 413 ? Codes.RESOURCE_TOO_LARGE : Codes.UNKNOWN), body?.error ?? `${xhr.status} ${xhr.statusText}`.trim(), xhr.status, body?.details));
      };
      xhr.onerror = () => {
        done();
        reject(new StructuredError(Codes.NETWORK, "The harness server is not reachable."));
      };
      xhr.onabort = () => {
        done();
        reject(abortError());
      };

      signal?.addEventListener("abort", onAbort);
      xhr.open("POST", base + path);
      xhr.setRequestHeader("Accept", "application/json");
      xhr.send(form);
    });
}
