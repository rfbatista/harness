// The "Design system" select of a repository's settings: None or one of the
// project's design systems, with the folder an export will write to.
//
//   <div x-data="repositoryDesignSystem" data-project-id="p1" data-repository-id="r1">
//
// Choosing applies at once, through set_repository_design_system, except a
// move from one design system to another, which asks first. Applying never
// writes to the repository, and removing leaves the exported files in place.

import { describeError } from "../../../../shared/presentation/errors.js";
import { DEFAULT_PATH } from "../../domain/repositories.js";

/** @param {{ gateway: ReturnType<typeof import("../../infrastructure/designsystem-gateway.js").designSystemGateway> }} deps */
export const repositoryDesignSystem = ({ gateway }) => () => ({
  projectId: "",
  repositoryId: "",
  /** @type {{ id: string, name: string, version: number }[]} */
  systems: [],
  /** The design system the repository uses ("" for None). */
  applied: "",
  /** What the select shows. */
  selected: "",
  path: "",
  draft: "",
  loaded: false,
  loadError: null,
  saving: false,
  error: null,
  /** The design system a move is waiting to confirm; "" when none. */
  moveToId: "",
  /** Name of the design system the repository uses, when the server named it in a refusal. */
  movedFrom: "",

  get noSystems() {
    return this.loaded && this.systems.length === 0;
  },
  get showPath() {
    return this.selected !== "";
  },
  get selectDisabled() {
    return this.noSystems || this.saving;
  },
  get pathVisible() {
    return this.loaded && this.showPath && !this.moveOpen;
  },
  get appliedName() {
    return this.nameOf(this.applied);
  },
  get moveOpen() {
    return this.moveToId !== "";
  },
  get moveFromName() {
    return this.movedFrom || this.appliedName;
  },
  get moveToName() {
    return this.nameOf(this.moveToId);
  },
  get cannotSavePath() {
    return this.saving || this.applied === "" || this.draft === this.path;
  },

  nameOf(id) {
    return this.systems.find((s) => s.id === id)?.name ?? "";
  },
  versionLabel(s) {
    return s.version > 0 ? "v" + s.version : "empty";
  },
  isSelected(id) {
    return this.selected === id;
  },

  async init() {
    this.projectId = this.$el.dataset.projectId ?? "";
    this.repositoryId = this.$el.dataset.repositoryId ?? "";
    await this.load();
  },

  async load() {
    this.loadError = null;
    try {
      this.systems = await gateway.designSystems(this.projectId);
      if (this.systems.length > 0) {
        const repo = await gateway.repository(this.repositoryId);
        this.applied = repo.designSystemId;
        this.path = repo.path || DEFAULT_PATH;
        this.draft = this.path;
      }
      this.selected = this.applied;
      this.loaded = true;
    } catch (err) {
      this.loadError = describeError(err);
    }
  },

  /** The select moved to id ("" is None). */
  async choose(id) {
    if (this.saving) return;
    this.error = null;
    this.moveToId = "";
    this.movedFrom = "";
    this.selected = id;
    if (id === this.applied) return;
    if (id !== "" && this.applied !== "") {
      this.moveToId = id;
      return;
    }
    await this.save(id, false);
  },

  async confirmMove() {
    const id = this.moveToId;
    this.moveToId = "";
    await this.save(id, true);
  },

  keepCurrent() {
    this.moveToId = "";
    this.movedFrom = "";
    this.selected = this.applied;
  },

  async savePath() {
    if (this.cannotSavePath) return;
    await this.save(this.applied, false);
  },

  /** Applies id ("" removes it), with the path as drafted when one is chosen and the saved one otherwise. */
  async save(id, confirmMove) {
    if (this.saving) return;
    this.saving = true;
    this.error = null;
    try {
      const stored = await gateway.apply(this.repositoryId, id === "" ? null : id, id === "" ? this.path : this.draft, confirmMove);
      this.applied = stored.designSystemId;
      this.selected = this.applied;
      this.path = stored.path || this.path;
      this.draft = this.path;
    } catch (err) {
      this.selected = this.applied;
      if (err?.code === "MOVE_NOT_CONFIRMED") {
        this.moveToId = id;
        this.movedFrom = err.details?.current?.name ?? "";
        this.selected = id;
      } else {
        this.error = describeError(err);
      }
    } finally {
      this.saving = false;
    }
  },
});
