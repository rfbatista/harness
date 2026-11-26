import { StructuredError } from "../../../../shared/domain/errors.js";
import { mount } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryBudgetOverride } from "./memoryBudgetOverride.js";

file("settings/presentation/components/memoryBudgetOverride");

const GB = 1024 ** 3;

/** A repository's control as the server paints it: inheriting 4 GB. */
function controlEl({ bytes = 0, effective = 4 * GB } = {}) {
  const el = document.createElement("div");
  el.dataset.level = "repository";
  el.dataset.ownerId = "r1";
  el.dataset.bytes = String(bytes);
  el.dataset.effective = String(effective);
  el.innerHTML = `<input type="number">`;
  el.querySelector("input").value = bytes ? String(bytes / GB) : "";
  return el;
}

test("an inheriting control says what it inherits", () => {
  const { instance } = mount(memoryBudgetOverride({ gateway: {} }), { el: controlEl() });
  instance.init();
  assert.equal(instance.summary, "Inherits 4 GB");
});

test("a value is saved for the owner and the summary follows the answer", async () => {
  const saved = [];
  const gateway = {
    setMemoryBudgetOverride: async (level, id, bytes) => (saved.push([level, id, bytes]), { bytes, effectiveBytes: bytes }),
  };
  const el = controlEl();
  const { instance } = mount(memoryBudgetOverride({ gateway }), { el });
  instance.init();
  const input = el.querySelector("input");
  input.value = "8";
  await instance.choose({ target: input });
  assert.deepEqual(saved, [["repository", "r1", 8 * GB]]);
  assert.equal(instance.summary, "Uses 8 GB");
});

test("emptying the field goes back to inheriting", async () => {
  const saved = [];
  const gateway = {
    setMemoryBudgetOverride: async (level, id, bytes) => (saved.push(bytes), { bytes, effectiveBytes: 4 * GB }),
  };
  const el = controlEl({ bytes: 8 * GB, effective: 8 * GB });
  const { instance } = mount(memoryBudgetOverride({ gateway }), { el });
  instance.init();
  const input = el.querySelector("input");
  input.value = "";
  await instance.choose({ target: input });
  assert.deepEqual(saved, [0]);
  assert.equal(instance.summary, "Inherits 4 GB");
});

test("a bad value or a refusal puts the field back", async () => {
  const gateway = { setMemoryBudgetOverride: async () => { throw new StructuredError("INVALID_INPUT", "no"); } };
  const el = controlEl({ bytes: 2 * GB, effective: 2 * GB });
  const { instance } = mount(memoryBudgetOverride({ gateway }), { el });
  instance.init();
  const input = el.querySelector("input");
  input.value = "-3";
  await instance.choose({ target: input });
  assert.equal(input.value, "2");
  assert.equal(instance.error.code, "INVALID_INPUT");
  input.value = "5";
  await instance.choose({ target: input });
  assert.equal(input.value, "2");
  assert.equal(instance.summary, "Uses 2 GB");
});
