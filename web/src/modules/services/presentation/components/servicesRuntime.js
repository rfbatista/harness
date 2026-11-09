// The container runtime choice on the Server page's Services pane: docker or
// podman, and whether its command line is found on the server. A change
// saves at once and applies to the next start of an image service.
//
//   <section x-data="servicesRuntime"> <select x-on:change="choose"> …

import { describeError } from "../../../../shared/presentation/errors.js";

/** @param {{ gateway: import("../../domain/ports.js").ServicesGateway }} deps */
export const servicesRuntime = ({ gateway }) => () => ({
  /** @type {"docker" | "podman"} */
  runtime: "docker",
  found: true,
  loading: true,
  saving: false,
  error: null,
  announcement: "",

  async init() {
    try {
      this.apply(await gateway.getSettings());
    } catch (err) {
      this.error = describeError(err);
    } finally {
      this.loading = false;
    }
  },

  apply(settings) {
    this.runtime = settings.container_runtime;
    this.found = settings.available;
  },

  get status() {
    return this.found
      ? `${this.runtime} was found on this machine.`
      : `${this.runtime} was not found on this machine. Image services cannot start until it is installed, or you choose ${this.runtime === "docker" ? "podman" : "docker"}.`;
  },

  /** @param {{ target: { value: string } }} event the select's change */
  async choose(event) {
    const wanted = event.target.value;
    if (this.saving || wanted === this.runtime) return;
    this.saving = true;
    this.error = null;
    try {
      this.apply(await gateway.setSettings(wanted));
      this.announcement = `Container runtime saved: ${this.runtime}.`;
    } catch (err) {
      this.error = describeError(err);
      event.target.value = this.runtime;
    } finally {
      this.saving = false;
    }
  },

  dismissError() {
    this.error = null;
  },
});
