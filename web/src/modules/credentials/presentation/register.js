// The credentials module's Alpine components.

import { gitCredentialsPage } from "./components/gitCredentialsPage.js";
import { repositoryCredential } from "./components/repositoryCredential.js";
import { repositoryCredentialsStore } from "./repositoryStore.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{
 *   gateway: import("../domain/ports.js").CredentialGateway,
 *   clock: import("../../../shared/infrastructure/clock.js").Clock,
 * }} deps
 */
export function registerCredentials(Alpine, deps) {
  Alpine.store("repositoryCredentials", repositoryCredentialsStore(deps));
  const store = Alpine.store("repositoryCredentials");
  Alpine.data("gitCredentialsPage", gitCredentialsPage(deps));
  Alpine.data("repositoryCredential", repositoryCredential({ store }));
}
