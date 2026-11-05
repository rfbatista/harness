// Wires the shared skeleton rule (shared/domain/skeletonRule.js) to a component.
// The component spreads skeletonFields() into its state and starts the rule in
// init(); its template shows the skeleton under `skeletonShown` and puts
// `skeletonIn` on the content so it fades in where the skeleton was:
//
//   { ...skeletonFields(), init() { skeleton = followSkeleton(this); … } }
//   <template x-if="skeletonShown"><div class="skeleton" data-variant="rows" role="status" aria-busy="true">…</div></template>
//   <div x-bind:class="skeletonIn">…</div>
//
// A load calls skeleton.begin(), then end() when the data is in, or fail() when
// it failed (the list's own error shows, never a skeleton that stays).

import { skeletonRule } from "../domain/skeletonRule.js";

export const skeletonFields = () => ({
  skeletonShown: false,
  /** The content arrived after a skeleton: it takes the fade-in class. */
  skeletonFaded: false,
  get skeletonIn() {
    return this.skeletonFaded ? "skeleton-in" : "";
  },
});

/**
 * @param {{ skeletonShown: boolean, skeletonFaded: boolean }} host  the component (`this` in init)
 * @param {import("../domain/skeletonRule.js").Timers} [timers]
 */
export function followSkeleton(host, timers) {
  return skeletonRule({
    timers,
    onChange: ({ visible, fade }) => {
      host.skeletonShown = visible;
      if (fade) host.skeletonFaded = true;
    },
  });
}
