// The lightbox, a part of the task's resources page (taskResources.js): an
// image resource at full size in the page's one modal, a native <dialog>
// (showModal) over the strong scrim. ← → step through the task's images, Esc
// or a click on the scrim closes it, and focus goes back to what opened it.
//
//   <dialog class="[ lightbox ]" x-ref="lightbox" x-on:close="lightboxClosed" …>

import { sizeWords, typeWord, viewableImages } from "../../domain/resources.js";

export function resourceLightbox() {
  /** What opened the lightbox, to give focus back to. Not reactive. */
  let opener = null;

  return {
    lightboxId: "",

    get lightboxImages() {
      return viewableImages(this.resources);
    },
    get lightboxIndex() {
      return this.lightboxImages.findIndex((r) => r.id === this.lightboxId);
    },
    get lightboxResource() {
      return this.lightboxImages[this.lightboxIndex] ?? null;
    },
    get lightboxSrc() {
      return this.lightboxResource?.contentUrl ?? "";
    },
    get lightboxTitle() {
      return this.lightboxResource?.title ?? "";
    },
    get lightboxMeta() {
      const r = this.lightboxResource;
      if (!r) return "";
      return `${typeWord(r)} · ${sizeWords(r.sizeBytes)} · ${this.lightboxIndex + 1} of ${this.lightboxImages.length}`;
    },
    get lightboxDownload() {
      return this.lightboxResource ? `${this.lightboxResource.contentUrl}?download=1` : "";
    },
    get lightboxSteps() {
      return this.lightboxImages.length > 1;
    },

    /** The tile or the title of an image row. */
    viewImage(event) {
      const id = event.target?.closest?.("[data-id]")?.dataset.id ?? "";
      this.openLightbox(id, event.currentTarget ?? event.target);
    },

    openLightbox(id, from = null) {
      if (!this.lightboxImages.some((r) => r.id === id)) return;
      opener = from;
      this.lightboxId = id;
      const dialog = this.$refs.lightbox;
      if (dialog && !dialog.open) dialog.showModal();
      this.focusLater("[data-lightbox-close]");
    },

    closeLightbox() {
      const dialog = this.$refs.lightbox;
      if (dialog?.open) dialog.close();
      else this.lightboxClosed();
    },

    /** The dialog closed (Close, Esc or the scrim): focus goes back. */
    lightboxClosed() {
      this.lightboxId = "";
      const back = opener;
      opener = null;
      if (back?.isConnected) back.focus();
    },

    /** A click on the dialog itself, not its content, is a click on the scrim. */
    lightboxClick(event) {
      if (event.target === event.currentTarget) this.closeLightbox();
    },

    stepLightbox(by) {
      const images = this.lightboxImages;
      if (images.length === 0) return;
      const at = (this.lightboxIndex + by + images.length) % images.length;
      this.lightboxId = images[at].id;
    },
    nextImage() {
      this.stepLightbox(1);
    },
    previousImage() {
      this.stepLightbox(-1);
    },

    /** The image on show was deleted elsewhere: show the next one, or close. */
    lightboxGone() {
      const images = this.lightboxImages;
      if (images.length <= 1) {
        this.closeLightbox();
        return;
      }
      this.stepLightbox(1);
    },
  };
}
