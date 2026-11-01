// An architecture proposal (the contract "Harness server ↔ Web UI —
// architecture assistant turns"): applying its changes to a record, finding
// the proposal conflicts against the person's own edits, and grouping the
// changes by the tab they touch. The server applies a proposal in the same
// way, so these rules follow the contract table. Pure.

import { TABS, cloneRecord, sameRecord } from "./record.js";

/**
 * @typedef {{ kind: string, [key: string]: string }} ProposalTarget
 * @typedef {{ target: ProposalTarget, op: "add" | "change" | "remove", value?: any, base: any, reason: string }} ProposalChange
 * @typedef {{ id: string, summary: string, valid: boolean, changes: ProposalChange[], errors: { path: string, code: string, message: string }[] }} Proposal
 */

/** The `*` agent of an access target: the layer's (or unlayered's) default. */
const DEFAULT_AGENT = "*";

/** The keyed lists of the record: where each kind lives and what names one item. */
const LISTS = {
  rule: { get: (r) => r.rules, set: (r, xs) => ((r.rules = xs), r), matches: (t) => (x) => x.id === t.id },
  instruction: { get: (r) => r.instructions, set: (r, xs) => ((r.instructions = xs), r), matches: (t) => (x) => x.path === t.path },
  tool: { get: (r) => r.tools, set: (r, xs) => ((r.tools = xs), r), matches: (t) => (x) => x.name === t.name },
  sandbox: { get: (r) => r.sandboxes, set: (r, xs) => ((r.sandboxes = xs), r), matches: (t) => (x) => x.name === t.name },
  layer: { get: (r) => r.layers, set: (r, xs) => ((r.layers = xs), r), matches: (t) => (x) => x.name === t.name },
  routing: {
    get: (r) => r.orchestration?.routing,
    set: (r, xs) => ((r.orchestration = { ...r.orchestration, routing: xs }), r),
    matches: (t) => (x) => x.agent === t.agent,
  },
  handoff: {
    get: (r) => r.orchestration?.handoffs,
    set: (r, xs) => ((r.orchestration = { ...r.orchestration, handoffs: xs }), r),
    matches: (t) => (x) => x.from === t.from && x.to === t.to,
  },
};

const without = (obj, key) => {
  const { [key]: _dropped, ...rest } = obj ?? {};
  return rest;
};

/** The access object a target names: a layer's, or the unlayered one. Null when the layer is not there. */
const accessHolder = (record, target) => {
  if (target.kind === "unlayered_access") return { get: () => record.unlayered, set: (a) => (record.unlayered = a) };
  const layer = (record.layers ?? []).find((l) => l.name === target.layer);
  return layer ? { get: () => layer.access, set: (a) => (layer.access = a) } : null;
};

function applyChange(record, change) {
  const { target, op, value } = change;
  switch (target.kind) {
    case "pattern":
      if (op === "remove") delete record.pattern;
      else record.pattern = value;
      return;
    case "layer_access":
    case "unlayered_access": {
      const holder = accessHolder(record, target);
      if (!holder) return;
      const access = holder.get() ?? { default: "read" };
      if (target.agent === DEFAULT_AGENT) {
        if (op !== "remove") holder.set({ ...access, default: value });
      } else {
        const agents = op === "remove" ? without(access.agents, target.agent) : { ...access.agents, [target.agent]: value };
        holder.set({ ...access, agents });
      }
      return;
    }
    default: {
      const list = LISTS[target.kind];
      if (!list) return;
      const items = [...(list.get(record) ?? [])];
      const at = items.findIndex(list.matches(target));
      if (op === "remove") {
        if (at >= 0) items.splice(at, 1);
      } else {
        // A layer change leaves the access as it is; every other value is the item whole.
        const next = target.kind === "layer" && op === "change" && at >= 0 ? withAccessOf(value, items[at]) : value;
        if (at >= 0) items[at] = next;
        else items.push(next);
      }
      list.set(record, items);
    }
  }
}

const withAccessOf = (value, existing) => {
  const next = without(value, "access");
  return existing.access === undefined ? next : { ...next, access: existing.access };
};

/**
 * The record with every change of the proposal applied in order. The given record is not touched.
 * @param {any} record
 * @param {Proposal} proposal
 */
export function applyProposal(record, proposal) {
  const out = cloneRecord(record);
  for (const change of proposal.changes) applyChange(out, change);
  return out;
}

/** The value a target has in a record, null when it is not there. A layer change compares the layer without its access. */
function valueAt(record, change) {
  const { target } = change;
  const found = (v) => (v === undefined ? null : v);
  switch (target.kind) {
    case "pattern":
      return found(record.pattern);
    case "layer_access":
    case "unlayered_access": {
      const access = accessHolder(record, target)?.get();
      if (!access) return null;
      return found(target.agent === DEFAULT_AGENT ? access.default : access.agents?.[target.agent]);
    }
    default: {
      const list = LISTS[target.kind];
      const item = list?.get(record)?.find(list.matches(target));
      if (item === undefined) return null;
      return target.kind === "layer" && change.op === "change" ? without(item, "access") : item;
    }
  }
}

/**
 * The value the draft has for a change's target once the change is applied: what a conflicting person's
 * value would be compared with.
 */
const proposedValueOf = (change) => (change.op === "remove" || change.value === undefined ? null : change.value);

/**
 * The changes that touch what the person has edited: the target's value in the current draft differs from
 * the change's base or from the saved record, and is not already what the change would make it.
 * @param {any} draft
 * @param {any} saved
 * @param {Proposal} proposal
 * @returns {{ index: number, change: ProposalChange, draftValue: any, savedValue: any, proposedValue: any }[]}
 */
export function proposalConflicts(draft, saved, proposal) {
  const out = [];
  proposal.changes.forEach((change, index) => {
    const draftValue = valueAt(draft, change);
    const savedValue = valueAt(saved, change);
    const proposedValue = proposedValueOf(change);
    const layerChange = change.target.kind === "layer" && change.op === "change";
    const base = layerChange && change.base ? without(change.base, "access") : (change.base ?? null);
    const edited = !sameRecord(draftValue, base) || !sameRecord(draftValue, savedValue);
    const shown = layerChange && proposedValue ? without(proposedValue, "access") : proposedValue;
    if (edited && !sameRecord(draftValue, shown)) out.push({ index, change, draftValue, savedValue, proposedValue });
  });
  return out;
}

/** The page's tab a change touches, or "pattern" for the page head's own field. */
const slugOf = (target) => {
  switch (target.kind) {
    case "pattern":
      return "pattern";
    case "layer":
    case "layer_access":
    case "unlayered_access":
      return "layers";
    case "rule":
      return "hooks";
    case "instruction":
      return "instructions";
    case "tool":
      return "tools";
    case "sandbox":
      return "sandboxes";
    default:
      return "orchestration";
  }
};

/**
 * A proposal's changes grouped by the tab they touch, in the page's tab order (the pattern first); each
 * change keeps its index in the proposal and its order inside the group.
 * @param {Proposal} proposal
 * @returns {{ slug: string, label: string, changes: (ProposalChange & { index: number })[] }[]}
 */
export function groupChanges(proposal) {
  const groups = [{ slug: "pattern", label: "Pattern", changes: [] }, ...TABS.map((t) => ({ slug: t.slug, label: t.label, changes: [] }))];
  proposal.changes.forEach((change, index) => groups.find((g) => g.slug === slugOf(change.target)).changes.push({ ...change, index }));
  return groups.filter((g) => g.changes.length > 0);
}
