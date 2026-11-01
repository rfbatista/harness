// The Proposals tab's model: a design session's proposals in words, the items
// of one with their base, current and proposed values, the before and after
// previews of what a change touches, and the contrast of a colour. Pure.

import { relativeTime } from "../../../shared/presentation/format.js";
import { previewDocument } from "./preview.js";

const list = (xs) => (Array.isArray(xs) ? xs : []);
const int = (n) => (Number.isInteger(n) ? n : 0);

/** @param {any} dto one entry of list_design_system_proposals */
export function proposalRowDto(dto) {
  return {
    id: dto.id ?? "",
    sessionId: dto.session_id ?? "",
    agent: dto.agent ?? "",
    sessionRunning: dto.session_running === true,
    state: dto.state ?? "",
    baseVersion: int(dto.base_version),
    summary: dto.summary ?? "",
    items: int(dto.items),
    conflicting: int(dto.conflicting),
    submittedAt: dto.submitted_at ?? "",
    decidedAt: dto.decided_at ?? "",
  };
}

/** @param {any} dto get_design_system_proposal's answer */
export function proposalViewDto(dto) {
  return {
    ...proposalRowDto(dto),
    currentVersion: int(dto.current_version),
    items: list(dto.items).map((it) => ({
      id: it.id,
      kind: it.kind,
      op: it.op,
      key: it.key,
      base: it.base ?? null,
      current: it.current ?? null,
      proposed: it.proposed ?? null,
      conflicting: it.conflicting === true,
      decision: it.decision ?? "pending",
    })),
    affected: list(dto.affected_components).map((a) => ({ slug: a.slug, byItems: list(a.by_items) })),
    proposedRecord: {
      tokens: list(dto.proposed_record?.tokens),
      principles: dto.proposed_record?.principles ?? "",
      components: list(dto.proposed_record?.components),
    },
  };
}

const STATES = {
  drafting: { label: "Drafting", tone: "" },
  open: { label: "Waiting", tone: "attention" },
  approved: { label: "Approved", tone: "success" },
  partly_approved: { label: "Partly approved", tone: "success" },
  rejected: { label: "Rejected", tone: "danger" },
  withdrawn: { label: "Withdrawn", tone: "" },
};

const plural = (n, one, many) => `${n} ${n === 1 ? one : many}`;

/** One line of the proposals list. */
export function rowView(row, now) {
  const state = STATES[row.state] ?? { label: row.state, tone: "" };
  const when = row.submittedAt === "" ? "" : relativeTime(new Date(row.submittedAt), now);
  const counts = row.state === "drafting" ? "not submitted yet" : plural(row.items, "change", "changes");
  const conflicts = row.conflicting > 0 ? ` · ${plural(row.conflicting, "conflict", "conflicts")}` : "";
  const session = row.sessionRunning ? "session running" : "session ended";
  return {
    id: row.id,
    state: row.state,
    label: state.label,
    tone: state.tone,
    agent: row.agent === "" ? "an agent" : row.agent,
    summary: row.state === "drafting" ? "Working on a draft…" : row.summary,
    detail: `${row.agent === "" ? "agent" : row.agent} · session ${row.sessionId.slice(0, 8)} (${session}) · ${counts}${conflicts} · based on version ${row.baseVersion}${when === "" ? "" : ` · ${when === "now" ? "just now" : `${when} ago`}`}`,
    decidable: row.state === "open",
    withdrawable: (row.state === "open" || row.state === "drafting") && !row.sessionRunning,
    greyed: row.state === "drafting",
    openable: row.state !== "drafting",
  };
}

const KINDS = { token: "Token", principles_section: "Principles", component: "Component" };
const OPS = { add: "added", change: "changed", remove: "removed" };

/** The key as it is read: a section's heading without its hashes. */
const keyText = (it) => (it.kind === "principles_section" ? it.key.replace(/^##\s*/, "") : it.key);

/** One entry of the changes list. */
export function itemView(it) {
  return {
    id: it.id,
    title: `${KINDS[it.kind] ?? it.kind} ${keyText(it)}`,
    kind: it.kind,
    kindLabel: KINDS[it.kind] ?? it.kind,
    keyText: keyText(it),
    op: it.op,
    opText: OPS[it.op] ?? it.op,
    conflicting: it.conflicting,
  };
}

/** A token's value in one line. */
export function valueText(token) {
  if (token === null || token === undefined) return "";
  const v = token.value;
  return v !== null && typeof v === "object" ? Object.entries(v).map(([k, x]) => `${k}: ${x}`).join(", ") : String(v);
}

/** The rows of a token item: what it was, what it is now, what it would be. */
export function tokenRows(it) {
  const row = (side, token) => ({ side, present: token !== null, value: valueText(token), note: token?.note ?? "", color: token?.type === "color" ? String(token.value) : "" });
  return [row("Base", it.base), row("Now", it.current), row("Proposed", it.proposed)];
}

/** Two texts as lines marked kept, removed or added (an unchanged line is kept). */
export function lineDiff(before, after) {
  const a = before === "" ? [] : before.replace(/\n$/, "").split("\n");
  const b = after === "" ? [] : after.replace(/\n$/, "").split("\n");
  const n = a.length;
  const m = b.length;
  const lcs = Array.from({ length: n + 1 }, () => new Array(m + 1).fill(0));
  for (let i = n - 1; i >= 0; i--) for (let j = m - 1; j >= 0; j--) lcs[i][j] = a[i] === b[j] ? lcs[i + 1][j + 1] + 1 : Math.max(lcs[i + 1][j], lcs[i][j + 1]);
  const out = [];
  let i = 0;
  let j = 0;
  while (i < n && j < m) {
    if (a[i] === b[j]) {
      out.push({ mark: " ", kind: "keep", text: a[i] });
      i++;
      j++;
    } else if (lcs[i + 1][j] >= lcs[i][j + 1]) {
      out.push({ mark: "-", kind: "del", text: a[i++] });
    } else {
      out.push({ mark: "+", kind: "add", text: b[j++] });
    }
  }
  while (i < n) out.push({ mark: "-", kind: "del", text: a[i++] });
  while (j < m) out.push({ mark: "+", kind: "add", text: b[j++] });
  return out.map((l, key) => ({ ...l, key }));
}

/** The diff of a principles item: its section's text before and after. */
export function sectionDiff(it) {
  return lineDiff(it.base?.markdown ?? "", it.proposed?.markdown ?? "");
}

/**
 * The before and after previews of what the proposal touches: the components
 * whose previews use a changed token, and the components it adds or changes.
 * `current` is the design system as it is now.
 */
export function previewPairs(view, current) {
  const slugs = new Set(view.affected.map((a) => a.slug));
  for (const it of view.items) if (it.kind === "component" && it.op !== "remove") slugs.add(it.key);
  const pairs = [];
  for (const slug of slugs) {
    const after = view.proposedRecord.components.find((c) => c.slug === slug);
    const before = current.components.find((c) => c.slug === slug) ?? null;
    if (!after) continue;
    after.previews.forEach((p, i) => {
      const was = before?.previews[i] ?? null;
      pairs.push({
        key: `${slug}#${i}`,
        slug,
        title: `${after.name} / ${p.label}`,
        isNew: before === null,
        before: was === null ? "" : previewDocument(current.tokens, was),
        after: previewDocument(view.proposedRecord.tokens, p),
      });
    });
  }
  return pairs;
}

// Contrast: the WCAG ratio of a colour on the canvas token, or on white.

const clamp01 = (x) => Math.min(1, Math.max(0, x));
const toLinear = (c) => (c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4);

/** A CSS colour as linear sRGB, or null for one this does not read (#hex, rgb(), oklch()). */
export function linearRgb(text) {
  const s = String(text).trim().toLowerCase();
  let m = /^#([0-9a-f]{3}|[0-9a-f]{6})$/.exec(s);
  if (m) {
    const h = m[1].length === 3 ? [...m[1]].map((c) => c + c).join("") : m[1];
    return [0, 2, 4].map((i) => toLinear(parseInt(h.slice(i, i + 2), 16) / 255));
  }
  m = /^rgba?\(\s*([\d.]+)[ ,]+([\d.]+)[ ,]+([\d.]+)/.exec(s);
  if (m) return [m[1], m[2], m[3]].map((x) => toLinear(clamp01(Number(x) / 255)));
  m = /^oklch\(\s*([\d.]+%?)\s+([\d.]+)\s+([\d.]+)/.exec(s);
  if (m) {
    const l = m[1].endsWith("%") ? Number(m[1].slice(0, -1)) / 100 : Number(m[1]);
    const c = Number(m[2]);
    const h = (Number(m[3]) * Math.PI) / 180;
    const a = c * Math.cos(h);
    const b = c * Math.sin(h);
    const l_ = (l + 0.3963377774 * a + 0.2158037573 * b) ** 3;
    const m_ = (l - 0.1055613458 * a - 0.0638541728 * b) ** 3;
    const s_ = (l - 0.0894841775 * a - 1.291485548 * b) ** 3;
    return [
      4.0767416621 * l_ - 3.3077115913 * m_ + 0.2309699292 * s_,
      -1.2684380046 * l_ + 2.6097574011 * m_ - 0.3413193965 * s_,
      -0.0041960863 * l_ - 0.7034186147 * m_ + 1.707614701 * s_,
    ].map(clamp01);
  }
  return null;
}

const luminance = ([r, g, b]) => 0.2126 * r + 0.7152 * g + 0.0722 * b;

/** The contrast ratio of two colours as "4.5:1", or "" when either is not a colour this reads. */
export function contrastText(foreground, background) {
  const f = linearRgb(foreground);
  const b = linearRgb(background);
  if (f === null || b === null) return "";
  const [hi, lo] = [luminance(f), luminance(b)].sort((x, y) => y - x);
  return `${((hi + 0.05) / (lo + 0.05)).toFixed(1)}:1`;
}

/** The colour a token is read on: the record's canvas, else white. */
function canvasOf(tokens) {
  const t = tokens.find((x) => x.type === "color" && x.name === "canvas") ?? tokens.find((x) => x.type === "color" && x.name === "background");
  return t ? { color: String(t.value), name: `${t.group}.${t.name}` } : { color: "#ffffff", name: "white" };
}

/** The contrast of a colour item before and after, on the canvas; null for any other item. */
export function contrastFigures(it, currentTokens, proposedTokens) {
  if (it.kind !== "token" || (it.proposed ?? it.base)?.type !== "color") return null;
  const canvas = canvasOf(proposedTokens);
  const was = it.base === null ? "" : contrastText(String(it.base.value), canvasOf(currentTokens).color);
  const now = it.proposed === null ? "" : contrastText(String(it.proposed.value), canvas.color);
  if (was === "" && now === "") return null;
  return { against: canvas.name, before: was, after: now };
}
