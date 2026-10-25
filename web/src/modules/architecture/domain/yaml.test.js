import { assert, file, test } from "../../../shared/testing/test.js";
import { YAML_HEADER, recordToYaml } from "./yaml.js";

file("architecture/domain/yaml");

test("the file starts with the generated-by comment and keys follow the contract's order", () => {
  const yaml = recordToYaml({
    rules: [],
    layers: [],
    unlayered: { default: "read", agents: {} },
    pattern: "hexagonal",
    repository: "harness",
    version: 1,
    orchestration: { handoffs: [], routing: [] },
    sandboxes: [],
    tools: [],
    instructions: [],
  });
  const keys = yaml
    .split("\n")
    .filter((l) => /^[a-z]/.test(l))
    .map((l) => l.split(":")[0]);
  assert.equal(yaml.split("\n")[0], YAML_HEADER);
  assert.equal(keys.join(), "version,repository,pattern,instructions,tools,sandboxes,orchestration,layers,unlayered,rules");
});

test("lists of objects keep their order and nested keys follow the documented order", () => {
  const yaml = recordToYaml({
    version: 1,
    repository: "harness",
    layers: [
      { access: { agents: { "software-architect": "change" }, default: "read" }, match: { folder: "internal/domain" }, name: "domain" },
      { name: "adapters", match: { regex: "^internal/adapter/" }, access: { default: "read" } },
    ],
    rules: [{ message: "m", mode: "blocking", kind: "command", id: "archtest", at: "before_commit", command: "go test ./..." }],
  });
  assert.equal(
    yaml,
    `${YAML_HEADER}
version: 1
repository: harness
layers:
  - name: domain
    match:
      folder: internal/domain
    access:
      default: read
      agents:
        software-architect: change
  - name: adapters
    match:
      regex: '^internal/adapter/'
    access:
      default: read
rules:
  - id: archtest
    kind: command
    at: before_commit
    command: go test ./...
    mode: blocking
    message: m
`,
  );
});

test("scalar lists are flow lists, empty ones are [] and {}", () => {
  const yaml = recordToYaml({ version: 1, tools: [{ name: "task", kind: "mcp", used_by: ["all"], writes: false }], sandboxes: [], unlayered: { default: "none", agents: {} } });
  assert.ok(yaml.includes("    used_by: [all]\n    writes: false\n"), yaml);
  assert.ok(yaml.includes("sandboxes: []\n"), yaml);
  assert.ok(yaml.includes("  agents: {}\n"), yaml);
});

test("multi-line strings are block scalars", () => {
  const yaml = recordToYaml({ version: 1, tools: [{ name: "task", usage: "Move the status.\nNever set done.\n" }, { name: "b", usage: "one\ntwo" }] });
  assert.ok(yaml.includes("    usage: |\n      Move the status.\n      Never set done.\n"), yaml);
  assert.ok(yaml.includes("    usage: |-\n      one\n      two\n"), yaml);
});

test("strings that would read as something else are quoted", () => {
  const yaml = recordToYaml({ version: 1, repository: "yes", pattern: "a: b", layers: [{ name: "x", purpose: "'fine' it's" }, { name: "12" }] });
  assert.ok(yaml.includes("repository: 'yes'\n"), yaml);
  assert.ok(yaml.includes("pattern: 'a: b'\n"), yaml);
  assert.ok(yaml.includes("purpose: '''fine'' it''s'\n"), yaml);
  assert.ok(yaml.includes("- name: '12'\n"), yaml);
});

test("the same record always gives the same text", () => {
  const rec = { version: 1, repository: "r", layers: [{ name: "a" }] };
  assert.equal(recordToYaml(rec), recordToYaml(JSON.parse(JSON.stringify(rec))));
});
