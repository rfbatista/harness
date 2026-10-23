import { mount } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { isolationSummary } from "./isolationSummary.js";

file("agentisolation/presentation/components/isolationSummary");

const seed = {
  credential: { text: "Credential: set", tone: "signal", blocks: false },
  claude: { text: "claude: not installed", tone: "danger", blocks: true },
  sandbox: { text: "Sandbox: on", tone: "signal", blocks: false },
};

let n = 0;
function mounted() {
  const id = `isolation-summary-test-${++n}`;
  const el = document.createElement("nav");
  el.dataset.seed = id;
  el.innerHTML = `<script type="application/json" id="${id}">${JSON.stringify(seed)}</script>`;
  document.body.append(el);
  const m = mount(isolationSummary(), { el });
  m.instance.init();
  return m.instance;
}

test("it starts from the server's chips and says agents are blocked when one blocks", () => {
  const s = mounted();
  assert.equal(s.claude.text, "claude: not installed");
  assert.equal(s.blocked, true);
});

test("a section announcing its chip replaces it, and fixing the last blocker clears the sentence", () => {
  const s = mounted();
  s.take({ detail: { section: "claude", text: "claude: 2.1.52", tone: "signal", blocks: false } });
  assert.deepEqual(s.claude, { text: "claude: 2.1.52", tone: "signal", blocks: false });
  assert.equal(s.blocked, false);
  s.take({ detail: { section: "credential", text: "Credential: missing", tone: "danger", blocks: true } });
  assert.equal(s.blocked, true);
});

test("an event naming no chip is ignored", () => {
  const s = mounted();
  s.take({ detail: { section: "take", text: "x", tone: "", blocks: false } });
  assert.equal(typeof s.take, "function");
});
