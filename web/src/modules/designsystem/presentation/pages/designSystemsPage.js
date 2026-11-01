// A project's Design systems page: its design systems as rows, and the form
// that makes a new one: name, slug (suggested from the name) and description,
// then a start.
//
//   <main x-data="designSystemsPage" data-seed="design-systems-seed">
//
// The server seeds the identities for the first paint; the repositories that
// use each come from the API. Only Start blank is live: Import and Ask an
// agent arrive with later slices and stay inert.

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
  /** The form: whether it is open, its step ("identity" or "start"), the start chosen before it opened ("" or "blank"), and what is typed. */
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
    return this.chosen === "blank" ? "Start blank" : "Continue";
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

  /** New design system: the identity form, then the starts. */
  openNew() {
    this.openForm("");
  },

  /** Start blank on the empty project's cards: the identity form, then straight to creating. */
  startFirst() {
    this.openForm("blank");
  },

  openForm(start) {
    this.formOpen = true;
    this.step = "identity";
    this.chosen = start;
    this.name = "";
    this.slug = "";
    this.slugTouched = false;
    this.description = "";
    this.clearFault();
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
    this.step = "start";
  },

  continueForm() {
    this.submit();
  },

  back() {
    this.step = "identity";
  },

  /** Creates the design system with nothing saved and opens its page. */
  async startBlank() {
    if (!this.canContinue) return;
    this.saving = true;
    this.clearFault();
    const slug = this.slug;
    try {
      await gateway.create(this.projectId, { name: this.name.trim(), slug, description: this.description.trim() });
      navigate(`${this.indexHref}/${encodeURIComponent(slug)}`);
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
