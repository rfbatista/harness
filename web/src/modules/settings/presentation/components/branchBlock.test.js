import { StructuredError } from "../../../../shared/domain/errors.js";
import { mount } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { branchBlock } from "./branchBlock.js";

file("settings/presentation/components/branchBlock");

/** A mode's markup as the server paints it. */
function modeEl(checked) {
  const el = document.createElement("li");
  el.dataset.setting = "branches.block.mode.implement";
  el.innerHTML = `<input type="checkbox">`;
  el.querySelector("input").checked = checked;
  return el;
}

test("a switched checkbox is saved under the mode's key as on or off", async () => {
  const saved = [];
  const gateway = { setBranchBlock: async (key, on) => (saved.push([key, on]), on) };
  const el = modeEl(true);
  const { instance } = mount(branchBlock({ gateway }), { el });
  instance.init();
  const box = el.querySelector("input");
  box.checked = false;
  await instance.choose({ target: box });
  box.checked = true;
  await instance.choose({ target: box });
  assert.deepEqual(saved, [
    ["branches.block.mode.implement", false],
    ["branches.block.mode.implement", true],
  ]);
});

test("a refused change puts the checkbox back and shows why", async () => {
  const gateway = { setBranchBlock: async () => { throw new StructuredError("INVALID_INPUT", "no"); } };
  const el = modeEl(true);
  const { instance } = mount(branchBlock({ gateway }), { el });
  instance.init();
  const box = el.querySelector("input");
  box.checked = false;
  await instance.choose({ target: box });
  assert.equal(box.checked, true);
  assert.equal(instance.error.code, "INVALID_INPUT");
  instance.dismissError();
  assert.equal(instance.error, null);
});
