// The Output tab of the Architecture page: a component nested in
// architecturePage, so it reads the page's `saved`, `dirty`, `exists`,
// `exportInfo`, `view`, `version` and `repositoryId` through the page's scope.
// It has two views, the architecture file and the diagram, which are deep
// links (`/architecture/file`, `/architecture/diagram`); the page opens them.

import { describeError } from "../../../../shared/presentation/errors.js";
import { describeExport, describeRefusal } from "../../domain/export.js";
import { recordToYaml } from "../../domain/yaml.js";
import { copyText } from "../../infrastructure/clipboard.js";

const shortSha = (sha) => String(sha).slice(0, 7);

/** @param { gateway: ReturnType<typeof import("../../infrastructure/architecture-gateway.js").architectureGateway> } deps */
export const outputTab = ({ gateway }) => () => ({
  exporting: false,
  /** What the last export did: { commit, path }, or null. */
  result: null,
  /** Why the last export was refused: { code, title, text, next }, or null. */
  refusal: null,
  copied: false,

  /** The open view: the diagram only when its deep link was opened. */
  get current() {
    return this.view === "diagram" ? "diagram" : "file";
  },
  get isFile() {
    return this.current === "file";
  },
  get isDiagram() {
    return this.current === "diagram";
  },
  get views() {
    return [
      { slug: "file", label: "File", pressed: this.isFile },
      { slug: "diagram", label: "Diagram", pressed: this.isDiagram },
    ];
  },

  /** The YAML the export writes: always the saved version, never the draft. */
  get yaml() {
    return this.saved ? recordToYaml(this.saved.record) : "";
  },
  get filePath() {
    return this.exportInfo?.path || ".harness/architecture.yaml";
  },
  get state() {
    return describeExport(this.exportInfo);
  },
  get hasState() {
    return this.state !== null && this.exists;
  },
  get stateTitle() {
    return this.state?.title ?? "";
  },
  get stateText() {
    return this.state?.text ?? "";
  },
  get stateNext() {
    return this.state?.next ?? "";
  },
  get hasLastExport() {
    return !!this.exportInfo?.exported_commit;
  },
  get lastExport() {
    const i = this.exportInfo ?? {};
    const when = i.exported_at ? new Date(i.exported_at).toLocaleString() : "";
    return [`version ${i.exported_version}`, `commit ${shortSha(i.exported_commit)}`, when].filter(Boolean).join(" · ");
  },
  get canExport() {
    return this.exists && !this.dirty && !this.exporting;
  },
  /** Why Export is not offered, or "". */
  get exportBlocked() {
    if (!this.exists) return "Save the architecture first: only a saved version can be exported.";
    if (this.dirty) return "Save your changes first: the file is written from the saved version, not from the draft.";
    return "";
  },
  get hasBlocked() {
    return this.exportBlocked !== "";
  },
  get exportLabel() {
    return this.exporting ? "Exporting…" : "Export to repository";
  },
  get hasResult() {
    return this.result !== null;
  },
  get resultText() {
    return this.result ? `Exported version ${this.version} as commit ${shortSha(this.result.commit)}. ${this.result.path} now matches the saved version.` : "";
  },
  get hasRefusal() {
    return this.refusal !== null;
  },
  get refusalIsStale() {
    return this.refusal?.code === "ARCHITECTURE_CHANGED";
  },
  get copyLabel() {
    return this.copied ? "Copied" : "Copy";
  },

  openFile() {
    this.openView("file");
  },
  openDiagram() {
    this.openView("diagram");
  },

  async exportFile() {
    if (!this.canExport) return;
    this.exporting = true;
    this.result = null;
    this.refusal = null;
    try {
      const done = await gateway.exportArchitecture(this.repositoryId, this.version);
      this.takeExport(done.export);
      this.result = { commit: done.commit, path: done.path };
    } catch (err) {
      const d = describeError(err);
      this.refusal = describeRefusal(err) ?? { code: d.code, title: "The export did not happen", text: d.message, next: d.next };
    } finally {
      this.exporting = false;
    }
  },

  async reloadPage() {
    this.refusal = null;
    await this.reload();
  },

  async copyYaml() {
    this.copied = await copyText(this.yaml);
    if (this.copied) setTimeout(() => (this.copied = false), 2000);
  },
});
