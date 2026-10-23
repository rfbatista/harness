import { StructuredError } from "../../../../shared/domain/errors.js";
import { mount } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { settingFlag } from "./settingFlag.js";

file("settings/presentation/components/settingFlag");

/** A flag's markup as the server paints it. */
function flagEl(checked) {
  const el = document.createElement("div");
  el.dataset.setting = "documents.agents_move_to_project";
  el.innerHTML = `<input type="checkbox">`;
  el.querySelector("input").checked = checked;
  return el;
}

test("a changed checkbox is saved under the flag's key", async () => {
  const saved = [];
  const gateway = { setFlag: async (key, on) => (saved.push([key, on]), on) };
  const el = flagEl(false);
  const { instance } = mount(settingFlag({ gateway }), { el });
  instance.init();
  const box = el.querySelector("input");
  box.checked = true;
  await instance.choose({ target: box });
  assert.deepEqual(saved, [["documents.agents_move_to_project", true]]);
  assert.equal(instance.value, true);
});

test("a refused change puts the checkbox back and shows why", async () => {
  const gateway = { setFlag: async () => { throw new StructuredError("INVALID_INPUT", "no"); } };
  const el = flagEl(false);
  const { instance } = mount(settingFlag({ gateway }), { el });
  instance.init();
  const box = el.querySelector("input");
  box.checked = true;
  await instance.choose({ target: box });
  assert.equal(box.checked, false);
  assert.equal(instance.error.code, "INVALID_INPUT");
});
