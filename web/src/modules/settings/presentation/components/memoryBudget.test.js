import { StructuredError } from "../../../../shared/domain/errors.js";
import { mount } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryBudget } from "./memoryBudget.js";

file("settings/presentation/components/memoryBudget");

const GB = 1024 ** 3;

/** The Server page's field as the server paints it: 4 GB stored. */
function fieldEl(bytes = 4 * GB) {
  const el = document.createElement("div");
  el.dataset.bytes = String(bytes);
  el.innerHTML = `<input type="number">`;
  el.querySelector("input").value = String(bytes / GB);
  return el;
}

test("a changed orçamento is saved in bytes", async () => {
  const saved = [];
  const gateway = { setMemoryBudget: async (bytes) => (saved.push(bytes), bytes) };
  const el = fieldEl();
  const { instance } = mount(memoryBudget({ gateway }), { el });
  instance.init();
  const input = el.querySelector("input");
  input.value = "6";
  await instance.choose({ target: input });
  assert.deepEqual(saved, [6 * GB]);
  assert.equal(instance.value, 6 * GB);
});

test("a fraction of a GB is allowed", async () => {
  const saved = [];
  const gateway = { setMemoryBudget: async (bytes) => (saved.push(bytes), bytes) };
  const el = fieldEl();
  const { instance } = mount(memoryBudget({ gateway }), { el });
  instance.init();
  const input = el.querySelector("input");
  input.value = "2.5";
  await instance.choose({ target: input });
  assert.deepEqual(saved, [2.5 * GB]);
});

test("a value that is not a positive number is not sent and the field goes back", async () => {
  const saved = [];
  const gateway = { setMemoryBudget: async (bytes) => (saved.push(bytes), bytes) };
  const el = fieldEl();
  const { instance } = mount(memoryBudget({ gateway }), { el });
  instance.init();
  const input = el.querySelector("input");
  for (const bad of ["", "0", "-1", "lots"]) {
    input.value = bad;
    await instance.choose({ target: input });
    assert.equal(input.value, "4", `field goes back after ${JSON.stringify(bad)}`);
    assert.equal(instance.error.code, "INVALID_INPUT");
  }
  assert.deepEqual(saved, []);
});

test("a refused save puts the field back and shows why", async () => {
  const gateway = { setMemoryBudget: async () => { throw new StructuredError("INVALID_INPUT", "no"); } };
  const el = fieldEl();
  const { instance } = mount(memoryBudget({ gateway }), { el });
  instance.init();
  const input = el.querySelector("input");
  input.value = "8";
  await instance.choose({ target: input });
  assert.equal(input.value, "4");
  assert.equal(instance.error.code, "INVALID_INPUT");
  assert.equal(instance.value, 4 * GB);
});
