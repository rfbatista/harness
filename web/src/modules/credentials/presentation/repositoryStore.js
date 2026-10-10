// What the Repositories page knows about its repositories' credentials: the
// credentials there are and what each repository resolves to. One per page,
// shared by every repository row (Alpine.store("repositoryCredentials")), so
// it follows the catalog feed once and re-reads the bindings in one request.
//
// Bindings are not on the feed: any credential change (or a missed stretch
// of the feed) can change what a repository resolves to, so the store reads
// them all again. Changes that arrive while a read is in flight cost one more
// read when it ends, never one per change.

import { Codes, codeOf } from "../../../shared/domain/errors.js";
import { FeedStatus } from "../../../shared/domain/feed.js";
import { describeError } from "../../../shared/presentation/errors.js";
import { applyChange } from "../domain/credential.js";

export const SEED_ID = "repository-credentials-seed";

/** @param {{ gateway: import("../domain/ports.js").CredentialGateway }} deps */
export function repositoryCredentialsStore({ gateway }) {
  let started = false;
  let reading = null;
  let again = false;

  return {
    projectId: "",
    /** @type {import("../domain/credential.js").Credential[]} */
    credentials: [],
    /** @type {Record<string, import("../domain/credential.js").RepositoryCredential>} */
    byRepository: {},
    locked: false,
    error: null,

    /** Reads the page's seed and follows the feed; only the first call does anything. */
    start(doc) {
      if (started) return;
      started = true;
      try {
        const script = doc.getElementById(SEED_ID);
        const seed = gateway.decodeRepositoriesSeed(JSON.parse(script?.textContent ?? "null"));
        this.projectId = seed.projectId;
        this.credentials = seed.credentials;
        this.locked = seed.locked;
        this.keep(seed.repositories);
      } catch (err) {
        this.error = describeError(err);
      }
      gateway.follow(
        (change) => {
          if (change.kind === "identity") return;
          this.credentials = applyChange(this.credentials, change);
          this.refresh();
        },
        (status) => status === FeedStatus.RESYNCED && this.refresh(),
      );
    },

    keep(list) {
      const next = { ...this.byRepository };
      for (const rc of list) next[rc.repositoryId] = rc;
      this.byRepository = next;
    },

    /** Reads every binding again; calls made meanwhile are folded into one more read. */
    refresh() {
      if (!this.projectId) return Promise.resolve();
      if (reading) {
        again = true;
        return reading;
      }
      reading = (async () => {
        do {
          again = false;
          try {
            this.keep(await gateway.listRepositoryCredentials(this.projectId));
            this.error = null;
          } catch (err) {
            this.error = describeError(err);
          }
        } while (again);
        reading = null;
      })();
      return reading;
    },

    /** Pins a credential (null: automatic). Rejects as the gateway does. */
    async pin(repositoryId, credentialId) {
      const rc = await gateway.setRepositoryCredential(repositoryId, credentialId);
      this.keep([rc]);
      return rc;
    },

    /** Tests what the repository resolves to against its origin. */
    async test(repositoryId) {
      const credentialId = this.byRepository[repositoryId]?.resolved?.id;
      if (!credentialId) return null;
      try {
        return await gateway.test(credentialId, repositoryId);
      } catch (err) {
        if (codeOf(err) === Codes.GIT_CREDENTIAL_STORE_LOCKED) this.locked = true;
        throw err;
      }
    },
  };
}
