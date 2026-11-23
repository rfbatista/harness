// The inbox page's filters: a GET form that works without JavaScript (a submit
// button in <noscript>); this submits it as soon as a choice changes.
//
//   <form method="get" action="/notifications" x-data="inboxFilters">
//     <select name="project" x-on:change="go">…</select>
//   </form>

export const inboxFilters = () => () => ({
  go() {
    this.$root.requestSubmit();
  },
});
