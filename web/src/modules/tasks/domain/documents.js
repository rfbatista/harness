// A task's documents, as the page watches them: which exist and in which
// version, to notice ones written after the page loaded.

/** @typedef {{ id: string, version: string }} DocumentVersion  version: updated_at as the API writes it */

/** The BFF's documentSignature (internal/adapter/in/web/tasks/documents.go). */
export function documentSignature(versions) {
  return versions
    .map((v) => `${v.id}@${v.version}`)
    .sort()
    .join(",");
}
