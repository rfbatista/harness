// The top bar's project picker. The picker is a GET form that works without
// JavaScript (a submit button in <noscript>); this submits it as soon as the
// choice changes.
//
//   <form method="get" action="/switch-project" x-data="projectPicker">
//     <select name="project" x-on:change="go">…</select>
//   </form>

export const projectPicker = () => () => ({
  go() {
    this.$root.requestSubmit();
  },
});
