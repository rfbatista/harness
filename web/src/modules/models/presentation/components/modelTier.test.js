import { StructuredError } from "../../../../shared/domain/errors.js";
import { mount } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { modelTier } from "./modelTier.js";

file("models/presentation/components/modelTier");

/** A tier's markup as the server paints it, at model. */
function tierEl(model) {
  const el = document.createElement("div");
  el.dataset.setting = "models.claude.architect";
  el.innerHTML = `<select><option value="default">default</option><option value="opus">opus</option><option value="sonnet">sonnet</option></select>`;
  el.querySelector("select").value = model;
  return el;
}

test("a chosen model is saved under the tier's key", async () => {
  const saved = [];
  const gateway = { setModel: async (key, model) => (saved.push([key, model]), model) };
  const el = tierEl("opus");
  const { instance } = mount(modelTier({ gateway }), { el });
  instance.init();
  const select = el.querySelector("select");
  select.value = "sonnet";
  await instance.choose({ target: select });
  assert.deepEqual(saved, [["models.claude.architect", "sonnet"]]);
  assert.equal(instance.value, "sonnet");
  assert.equal(instance.announcement, "Model set to sonnet.");
});

test("a refused model puts the select back and shows why", async () => {
  const gateway = { setModel: async () => { throw new StructuredError("INVALID_INPUT", "no such model"); } };
  const el = tierEl("opus");
  const { instance } = mount(modelTier({ gateway }), { el });
  instance.init();
  const select = el.querySelector("select");
  select.value = "sonnet";
  await instance.choose({ target: select });
  assert.equal(select.value, "opus");
  assert.equal(instance.value, "opus");
  assert.equal(instance.error.code, "INVALID_INPUT");
  assert.equal(instance.saving, false);
});
