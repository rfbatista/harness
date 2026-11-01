import { assert, file, test } from "../../../shared/testing/test.js";

import { dependencyFlags } from "./proposals.js";

file("designsystem/domain/proposals");

const ink = { id: "token:colors.ink", kind: "token", op: "add", key: "colors.ink", decision: "pending", proposed: { group: "colors", name: "ink" } };
const chip = {
  id: "component:chip",
  kind: "component",
  op: "add",
  key: "chip",
  decision: "pending",
  proposed: { previews: [{ css: ".x{color:var( --colors-ink )}" }] },
};
const view = { items: [ink, chip] };

test("a component approved while the token it uses is rejected or unmarked is flagged, naming the token", () => {
  assert.ok(/colors\.ink.*rejecting/.test(dependencyFlags(view, { [chip.id]: "approve", [ink.id]: "reject" })[chip.id]));
  assert.ok(/colors\.ink.*not approved/.test(dependencyFlags(view, { [chip.id]: "approve" })[chip.id]));
});

test("approving the token in the same apply, or having it approved already, clears the flag", () => {
  assert.deepEqual(dependencyFlags(view, { [chip.id]: "approve", [ink.id]: "approve" }), {});
  assert.deepEqual(dependencyFlags({ items: [{ ...ink, decision: "approved" }, chip] }, { [chip.id]: "approve" }), {});
});

test("a component not marked approve is not flagged", () => {
  assert.deepEqual(dependencyFlags(view, { [ink.id]: "reject" }), {});
  assert.deepEqual(dependencyFlags(view, { [chip.id]: "reject" }), {});
});
