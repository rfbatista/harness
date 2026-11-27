// A repository's Guard line on the pages that list repositories. It asks the
// agent-system API once the page is up, not the server's render: the effective
// configuration may read the checkout (gate detection), which a list of
// repositories must not wait for.
//
//   <span x-data="agentSystemGuardLine('<project id>', '<repository id>')" x-show="shown" data-guard-line x-bind:data-state="state" x-text="text"></span>
//
// or, in server-drawn markup, with data-project and data-repository on the root:
//
//   <span x-data="agentSystemGuardLine" data-project="…" data-repository="…" …>

import { loadingLine } from "../../domain/guardLine.js";

/** @param {{ gateway: ReturnType<typeof import("../../infrastructure/guard-gateway.js").guardGateway> }} deps */
export const guardLineComponent = ({ gateway }) => (projectId = "", repositoryId = "") => ({
  projectId,
  repositoryId,
  /** @type {import("../../domain/guardLine.js").GuardLine} */
  line: loadingLine(),
  /** The read failed: the line says nothing rather than something wrong. */
  failed: false,

  async init() {
    const data = this.$el?.dataset ?? {};
    this.projectId = this.projectId || data.project || "";
    this.repositoryId = this.repositoryId || data.repository || "";
    if (this.projectId === "" || this.repositoryId === "") return;
    try {
      this.line = await gateway.line(this.projectId, this.repositoryId);
    } catch {
      this.failed = true;
    }
  },

  /** Drawn once it is read: nothing while it loads or when the read failed. */
  get shown() {
    return !this.failed && this.line.state !== "loading";
  },
  get state() {
    return this.line.state;
  },
  get red() {
    return this.line.state === "red";
  },
  get text() {
    return this.line.text;
  },
  get href() {
    return `/projects/${encodeURIComponent(this.projectId)}/agent-system?repository=${encodeURIComponent(this.repositoryId)}`;
  },
});
