// The instruction files' gateway over /api and the project feed: the seven
// endpoints and the instruction_file_changed event of "Contract: Harness
// server ↔ Web UI — instruction files". The wire shapes are read here and
// nowhere else. Refusals arrive as StructuredErrors with the contract's codes;
// INSTRUCTION_INVALID also carries `fieldErrors`.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { fieldErrorsOf } from "../domain/instructionFiles.js";

/** @param {any} body @returns {{ path: string, kind: string, version: number, size: number, updatedAt: string }} */
function toMeta(body) {
  if (!body || typeof body.path !== "string" || typeof body.kind !== "string") {
    throw new StructuredError(Codes.BAD_RESPONSE, "The instruction file came back unreadable.");
  }
  return {
    path: body.path,
    kind: body.kind,
    version: Number.isInteger(body.version) ? body.version : 0,
    size: Number.isInteger(body.size) ? body.size : 0,
    updatedAt: body.updated_at ?? "",
  };
}

const list = (xs) => (Array.isArray(xs) ? xs : []);

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @param {import("../../../shared/infrastructure/feed.js").Feed} feed
 */
export function instructionFilesGateway(api, feed) {
  /** Posts; an INSTRUCTION_INVALID refusal gets its field errors read. */
  const post = async (route, body) => {
    try {
      return await api.post(route, body);
    } catch (err) {
      if (err instanceof StructuredError && err.code === "INSTRUCTION_INVALID") err.fieldErrors = fieldErrorsOf(err.details);
      throw err;
    }
  };

  return {
    /** The harness's files (metadata only) and the repository's copies of instruction-like files. */
    async list(repositoryId) {
      const body = await post("/list_instruction_files", { repository_id: repositoryId });
      return {
        files: list(body?.files).map(toMeta),
        repositoryCopies: list(body?.repository_copies).map((c) => ({ path: c?.path ?? "", kind: c?.kind ?? "", differs: c?.differs === true })),
      };
    },

    /** One file with its content. */
    async get(repositoryId, path) {
      const body = await post("/get_instruction_file", { repository_id: repositoryId, path });
      return { ...toMeta(body), content: typeof body.content === "string" ? body.content : "" };
    },

    /** Saves a file's text against the version it started from; base version 0 creates it. */
    async save(repositoryId, { path, kind, baseVersion, content }) {
      return toMeta(await post("/save_instruction_file", { repository_id: repositoryId, path, kind, base_version: baseVersion, content }));
    },

    async rename(repositoryId, { path, newPath, baseVersion }) {
      return toMeta(await post("/rename_instruction_file", { repository_id: repositoryId, path, new_path: newPath, base_version: baseVersion }));
    },

    /** Deletes a file; the answer has no body. */
    async remove(repositoryId, { path, baseVersion }) {
      await post("/delete_instruction_file", { repository_id: repositoryId, path, base_version: baseVersion });
    },

    /** Copies files from the checkout's default branch into the harness. */
    async importFiles(repositoryId, paths) {
      const body = await post("/import_instruction_files", { repository_id: repositoryId, paths });
      return list(body?.files).map(toMeta);
    },

    /** Deletes imported files from the checkout and commits, with no push; refusals (CHECKOUT_DIRTY, WRONG_BRANCH, INSTRUCTION_NOT_IMPORTED) arrive as errors. */
    async removeFromRepository(repositoryId, paths) {
      const body = await post("/remove_instruction_files_from_repository", { repository_id: repositoryId, paths });
      return { commit: body?.commit ?? "" };
    },

    /** Follows the project feed for one repository's instruction file changes: { path, version }, version 0 for a delete or the old path of a rename. */
    follow(projectId, repositoryId, { onChanged, onStatus }) {
      return feed.follow(`/events?project_id=${encodeURIComponent(projectId)}`, {
        onMessage(dto) {
          const changed = dto?.instruction_file_changed;
          if (changed && changed.repository_id === repositoryId && typeof changed.path === "string") {
            onChanged?.({ path: changed.path, version: Number.isInteger(changed.version) ? changed.version : 0 });
          }
        },
        onStatus,
      });
    },
  };
}
