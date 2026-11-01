// A project's Design systems page: its design systems as rows, and the form
// that makes a new one: name, slug (suggested from the name) and description,
// then a start.
//
//   <main x-data="designSystemsPage" data-seed="design-systems-seed">
//
// The server seeds the identities for the first paint; the repositories that
// use each come from the API. Start blank and Import are live: Import offers
// the repositories that hold a DESIGN.md or tokens.json, previews what parsed
// and creates the design system when confirmed. Ask an agent arrives with a
// later slice and stays inert.

import { FeedStatus } from "../../../../shared/domain/feed.js";
import { describeError } from "../../../../shared/presentation/errors.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { faultOf, listRow, sortRows, suggestSlug } from "../../domain/identity.js";

/** @param {{ gateway: ReturnType<typeof import("../../infrastructure/designsystem-gateway.js").designSystemGateway>, navigate: (url: string) => void }} deps */
export const designSystemsPage = ({ gateway, navigate }) => () => ({
  projectId: "",
  indexHref: "",
  rows: [],
  error: null,
  /** The server's first paint is replaced by the page's own rows. */
  live: false,
  /** The form: whether it is open, its step ("identity", "start" or "preview"), the start chosen before it opened ("", "blank" or "import"), and what is typed. */
  formOpen: false,
  step: "identity",
  chosen: "",
  name: "",
  slug: "",
  /** The slug was typed by the person: it no longer follows the name. */
  slugTouched: false,
  description: "",
  /** What the server refused, by field (null: nothing), and a refusal that names no field. */
  fault: null,
  formError: null,
  saving: false,
  unfollow: null,
  /** Import: the repositories that hold design system files, the one chosen with its paths, and the preview read from it (null: none). */
  sources: [],
  repositoryId: "",
  designMdPath: "",
  tokensPath: "",
  componentsDir: "",
  preview: null,
  previewing: false,

  get rowsView() {
    return sortRows(this.rows).map((r) => ({ ...r, href: `${this.indexHref}/${encodeURIComponent(r.slug)}` }));
  },
  get hasRows() {
    return this.rows.length > 0;
  },
  get isEmpty() {
    return this.rows.length === 0;
  },
  get showStarts() {
    return this.isEmpty && !this.formOpen;
  },
  get showNew() {
    return this.hasRows;
  },
  get identityStep() {
    return this.step === "identity";
  },
  get startStep() {
    return this.step === "start";
  },
  get submitLabel() {
    return { blank: "Start blank", agent: "Create and ask an agent" }[this.chosen] ?? "Continue";
  },
  get canContinue() {
    return !this.saving && this.name.trim() !== "" && this.slug !== "";
  },
  get nameFault() {
    return this.fault?.field === "name" ? this.fault : null;
  },
  get slugFault() {
    return this.fault?.field === "slug" ? this.fault : null;
  },
  get descriptionFault() {
    return this.fault?.field === "description" ? this.fault : null;
  },
  get canImport() {
    return this.sources.length > 0;
  },
  /** Import is the primary start when a repository holds the files; Start blank is then secondary. */
  get importVariant() {
    return this.canImport ? "primary" : null;
  },
  get blankVariant() {
    return this.canImport ? null : "primary";
  },
  get previewStep() {
    return this.step === "preview";
  },
  get canPreview() {
    return !this.previewing && this.repositoryId !== "";
  },
  get previewTitle() {
    return `Import into “${this.name.trim()}”`;
  },
  get confirmLabel() {
    return `Import as ${this.name.trim()}`;
  },
  get previewSummary() {
    const p = this.preview;
    return p ? `${p.tokenCount} tokens, ${p.sections.length} principle sections, ${p.components} components` : "";
  },
  get hasSkipped() {
    return (this.preview?.skipped.length ?? 0) > 0;
  },
  get skippedTitle() {
    return `Skipped (${this.preview?.skipped.length ?? 0})`;
  },
  get startsName() {
    return this.name.trim();
  },

  init() {
    try {
      const seed = readSeed(this.$el);
      this.projectId = seed.project_id;
      this.indexHref = seed.index_href;
      this.rows = (seed.design_systems ?? []).map(listRow);
    } catch (err) {
      this.error = describeError(err);
      return;
    }
    this.$nextTick(() => {
      for (const node of this.$el.querySelectorAll("[data-ssr]")) node.remove();
      this.live = true;
    });
    this.load();
    this.loadSources();
    this.unfollow = gateway.followProject(this.projectId, {
      onChanged: () => this.load(),
      onStatus: (status) => {
        if (status === FeedStatus.RESYNCED) this.load();
      },
    });
  },

  destroy() {
    this.unfollow?.();
  },

  /** Reads the list with the repositories that use each. */
  async load() {
    try {
      this.rows = await gateway.list(this.projectId);
      this.error = null;
    } catch (err) {
      this.error = describeError(err);
    }
  },

  /** Reads the repositories an import can start from; none when they cannot be read. */
  async loadSources() {
    try {
      this.sources = await gateway.importSources(this.projectId);
    } catch {
      this.sources = [];
    }
    if (!this.sources.some((s) => s.id === this.repositoryId)) this.pickSource(this.sources[0]);
  },

  /** Chooses a repository and fills the paths found there. */
  pickSource(source) {
    this.repositoryId = source?.id ?? "";
    this.designMdPath = source?.designMd || "DESIGN.md";
    this.tokensPath = source?.tokens || "tokens.json";
    this.componentsDir = "components/";
  },

  chooseRepository(event) {
    this.pickSource(this.sources.find((s) => s.id === event.target.value));
  },

  typeDesignMdPath(event) {
    this.designMdPath = event.target.value;
  },

  typeTokensPath(event) {
    this.tokensPath = event.target.value;
  },

  typeComponentsDir(event) {
    this.componentsDir = event.target.value;
  },

  /** New design system: the identity form, then the starts. */
  openNew() {
    this.openForm("");
  },

  /** Start blank on the empty project's cards: the identity form, then straight to creating. */
  startFirst() {
    this.openForm("blank");
  },

  /** Ask an agent on the empty project's cards: the identity form, then the spawn form on the new page. */
  startAgent() {
    this.openForm("agent");
  },

  /** Import on the empty project's cards: the identity form, then the import card. */
  startImport() {
    this.openForm("import");
  },

  openForm(start) {
    this.formOpen = true;
    this.step = "identity";
    this.chosen = start;
    this.name = "";
    this.slug = "";
    this.slugTouched = false;
    this.description = "";
    this.preview = null;
    this.clearFault();
    this.loadSources();
    this.$nextTick(() => this.$el.querySelector("[data-new-form] [data-field=name]")?.focus());
  },

  cancel() {
    this.formOpen = false;
  },

  clearFault() {
    this.fault = null;
    this.formError = null;
  },

  typeName(event) {
    this.name = event.target.value;
    if (!this.slugTouched) this.slug = suggestSlug(this.name);
    this.clearFault();
  },

  typeSlug(event) {
    this.slug = event.target.value;
    this.slugTouched = true;
    this.clearFault();
  },

  typeDescription(event) {
    this.description = event.target.value;
  },

  /** The identity form's one button: Continue to the starts, or the start already chosen. */
  submit() {
    if (!this.canContinue) return;
    if (this.chosen === "blank") return this.startBlank();
    if (this.chosen === "agent") return this.startWithAgent();
    this.step = "start";
  },

  continueForm() {
    this.submit();
  },

  back() {
    this.step = "identity";
  },

  get importPaths() {
    return { designMd: this.designMdPath.trim(), tokens: this.tokensPath.trim(), components: this.componentsDir.trim() };
  },

  /** Reads the repository and shows what would be stored; nothing is written. */
  async previewImport() {
    if (!this.canPreview) return;
    this.previewing = true;
    this.clearFault();
    try {
      this.preview = await gateway.previewImport(this.projectId, this.repositoryId, this.importPaths);
      this.step = "preview";
    } catch (err) {
      this.formError = { ...describeError(err), code: err?.code ?? "" };
    } finally {
      this.previewing = false;
    }
  },

  /** Leaves the preview for the import card; nothing was stored. */
  cancelPreview() {
    this.preview = null;
    this.step = "start";
  },

  /** Creates the design system from the previewed files and opens its page. */
  async confirmImport() {
    if (!this.canContinue || this.preview === null) return;
    this.saving = true;
    this.clearFault();
    const slug = this.slug;
    try {
      await gateway.importDesignSystem(this.projectId, this.repositoryId, this.importPaths, { name: this.name.trim(), slug, description: this.description.trim() });
      navigate(`${this.indexHref}/${encodeURIComponent(slug)}`);
    } catch (err) {
      const fault = faultOf(err);
      if (fault.field === "") {
        this.formError = { ...describeError(err), code: fault.code };
        this.step = "start";
      } else {
        this.fault = fault;
        this.step = "identity";
      }
    } finally {
      this.saving = false;
    }
  },

  /** Creates the design system with nothing saved and opens its page. */
  async startBlank() {
    return this.createAndOpen("");
  },

  /** Creates the design system at version 0 and opens its page on the spawn form. */
  async startWithAgent() {
    return this.createAndOpen("?spawn=1");
  },

  async createAndOpen(query) {
    if (!this.canContinue) return;
    this.saving = true;
    this.clearFault();
    const slug = this.slug;
    try {
      await gateway.create(this.projectId, { name: this.name.trim(), slug, description: this.description.trim() });
      navigate(`${this.indexHref}/${encodeURIComponent(slug)}${query}`);
    } catch (err) {
      const fault = faultOf(err);
      this.step = "identity";
      if (fault.field === "") this.formError = { ...describeError(err), code: fault.code };
      else this.fault = fault;
    } finally {
      this.saving = false;
    }
  },
});
