// CredentialGateway over the harness HTTP API (the Web UI contract "git
// credentials") and the catalog feed (/api/project_events), which it shares
// with the projects module.

import {
  toChange,
  toCredential,
  toCredentials,
  toIdentity,
  toPageSeed,
  toRepositoriesSeed,
  toRepositoryCredential,
  toRepositoryCredentials,
  toTestResult,
} from "./dto.js";

/** Drops absent fields so the server keeps their value (update's rule). */
const present = (o) => Object.fromEntries(Object.entries(o).filter(([, v]) => v !== undefined));

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @param {import("../../../shared/infrastructure/feed.js").Feed} events
 * @returns {import("../domain/ports.js").CredentialGateway}
 */
export function credentialsGateway(api, events) {
  return {
    async list() {
      return toCredentials(await api.get("/list_git_credentials"));
    },

    async create({ name, kind, host, username, token, privateKey, passphrase, generate, isDefault }) {
      const body = await api.post(
        "/create_git_credential",
        present({ name, kind, host, username: username || undefined, token, private_key: privateKey, passphrase: passphrase || undefined, generate, is_default: isDefault }),
      );
      return toCredential(body?.credential);
    },

    async update({ credentialId, name, host, username, token, privateKey, passphrase, isDefault }) {
      const body = await api.post(
        "/update_git_credential",
        present({ credential_id: credentialId, name, host, username, token: token || undefined, private_key: privateKey || undefined, passphrase: passphrase || undefined, is_default: isDefault }),
      );
      return toCredential(body?.credential);
    },

    async remove(credentialId) {
      await api.post("/delete_git_credential", { credential_id: credentialId });
    },

    async test(credentialId, repositoryId) {
      return toTestResult(await api.post("/test_git_credential", present({ credential_id: credentialId, repository_id: repositoryId || undefined })));
    },

    async getIdentity() {
      return toIdentity((await api.get("/get_git_identity"))?.identity);
    },

    async updateIdentity({ name, email }) {
      return toIdentity((await api.post("/update_git_identity", { name, email }))?.identity);
    },

    async getRepositoryCredential(repositoryId) {
      return toRepositoryCredential(await api.get("/get_repository_credential", { repository_id: repositoryId }));
    },

    async listRepositoryCredentials(projectId) {
      return toRepositoryCredentials(await api.get("/list_repository_credentials", { project_id: projectId }));
    },

    async setRepositoryCredential(repositoryId, credentialId) {
      return toRepositoryCredential(await api.post("/set_repository_credential", { repository_id: repositoryId, credential_id: credentialId }));
    },

    decodePageSeed: toPageSeed,
    decodeRepositoriesSeed: toRepositoriesSeed,

    follow(onChange, onStatus = () => {}) {
      return events.follow("/project_events", {
        onMessage(data) {
          let change;
          try {
            change = toChange(data);
          } catch {
            return; // a malformed message is skipped; the stream goes on
          }
          if (change) onChange(change);
        },
        onStatus,
      });
    },
  };
}
