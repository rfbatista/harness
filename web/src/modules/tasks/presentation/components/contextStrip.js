// The task page's context strip: which band's panel is open, if any. At most
// one is, so the strip's height stays bounded. The bands (feature, reviews,
// token savers) sit inside it and read `expanded` and call `toggle`.
//
//   <div x-data="tasksContextStrip" role="group" aria-label="Task context"> …bands…

export const contextStrip = () => () => ({
  /** The open band's key ("feature"); "" when none is. */
  open: "",

  expanded(key) {
    return this.open === key;
  },

  /** A summary button was pressed: open its panel, or close it when it is the open one. */
  toggle(key) {
    this.open = this.open === key ? "" : key;
  },
});
