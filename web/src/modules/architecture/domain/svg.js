// A Diagram model (domain/diagram.js) as standalone SVG text. Pure: it lays
// the model out itself and draws it, so the same code makes the picture on the
// page and the file the person downloads. Agents stand in a column on the left,
// grouped by the sandbox they run in (an agent in several sandboxes stands in
// the first); layers stand in a column on the right, edges fan between them
// and one guard sits on each edge at the layer's end. A sandbox is a frame
// round its agents (round the layers too when one sandbox holds every agent).
// Tools stand in a panel on the right: MCP servers and APIs in a Harness host
// box, functions and CLIs in a box inside the sandbox. Elements carry data
// attributes (data-agent, data-layer, data-edge, data-guard, data-region,
// data-tool) for the page to hook onto.

import { agentSlot } from "./layers.js";

const AX = 80; // agent column: room on its left for the hand-off arcs
const AGENT_W = 270;
const NODE_H = 46;
const AGENT_PITCH = 58;
const LX = 520; // layer column
const LAYER_W = 260;
const LAYER_H = 46;
const LAYER_PITCH = 64;
const PAD = 14; // between a frame and what it holds
const BAND = 26; // a frame's title band, above its first node
const GROUP_GAP = 16; // between two frames
const TOP = 36; // room above the frames for the draft pill and the host tool lane
const TX = 836; // tools panel
const TOOLS_W = 324;
const TOOL_W = 200;
const TOOL_PITCH = 64;

// The picture's colours as literals, on purpose: the same text is downloaded as
// a standalone SVG file, which cannot read the page's CSS variables (no design
// tokens outside the page), so every colour is gathered here and nowhere else.
// The page paints the picture from tokens through the dg-* classes instead.
const PALETTE = {
  ink: "#1f2937",
  muted: "#6b7280",
  change: "#2563eb",
  read: "#94a3b8",
  paper: "#ffffff",
  sandbox: "#f4f6fa",
  host: "#eef1f5",
  layer: "#f8fafc",
  focused: "#dbeafe",
  block: { fill: "#fde8e8", stroke: "#b42318", ink: "#7a1d14" },
  warn: { fill: "#fff1cc", stroke: "#b54708", ink: "#7a3b05" },
  // The agent colours, one per slot (the matrix chips use the same slots).
  agents: ["#7c4dbd", "#0e7f8f", "#2b78b5", "#c0702a", "#c04a8d", "#2f8a58", "#566a85", "#8a8a2a"],
};
const HANDOFF = "#7c3aed"; // hand-off arrows: their own hue, neither change nor read
const { ink: INK, muted: MUTED, change: CHANGE, block: BLOCK, warn: WARN } = PALETTE;

const esc = (s) => String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
const num = (n) => Math.round(n * 10) / 10;
const plural = (n, one, many) => `${n} ${n === 1 ? one : many}`;

/** The sandboxes that hold an agent, in the record's order. */
const sandboxesOf = (d, agent) => d.regions.filter((r) => r.agents.includes(agent)).map((r) => r.name);

/**
 * The room a diagram takes and where each node sits; exported for the page's tests.
 * regions: name to { x, y, w, h, label } (label is the box the title takes up).
 */
export function layoutDiagram(d) {
  const home = (name) => d.regions.findIndex((r) => r.agents.includes(name));
  const groups = d.regions.map((r, i) => ({ region: r, agents: d.nodes.agents.filter((a) => home(a.name) === i) })).filter((g) => g.agents.length > 0);
  const loose = d.nodes.agents.filter((a) => home(a.name) < 0);

  const agents = new Map();
  const regions = new Map();
  let y = TOP;
  const frames = [];
  for (const g of groups) {
    const top = y;
    y += BAND;
    for (const a of g.agents) {
      agents.set(a.name, { x: AX, y, w: AGENT_W, h: NODE_H });
      y += AGENT_PITCH;
    }
    const bottom = y - AGENT_PITCH + NODE_H + PAD;
    frames.push({ name: g.region.name, top, bottom });
    y = bottom + GROUP_GAP;
  }
  for (const a of loose) {
    agents.set(a.name, { x: AX, y, w: AGENT_W, h: NODE_H });
    y += AGENT_PITCH;
  }
  const agentsBottom = loose.length > 0 ? y - AGENT_PITCH + NODE_H : frames.length > 0 ? frames[frames.length - 1].bottom : TOP;

  const firstY = groups.length > 0 ? TOP + BAND : TOP;
  const layers = new Map(d.nodes.layers.map((l, i) => [l.name, { x: LX, y: firstY + i * LAYER_PITCH, w: LAYER_W, h: LAYER_H }]));
  const layersBottom = firstY + (d.nodes.layers.length - 1) * LAYER_PITCH + LAYER_H;

  const single = frames.length === 1 && loose.length === 0;
  for (const f of frames) {
    const x = AX - PAD;
    const w = single ? LX + LAYER_W + PAD - x : AGENT_W + 2 * PAD;
    const bottom = single ? Math.max(f.bottom, layersBottom + PAD) : f.bottom;
    regions.set(f.name, { x, y: f.top, w, h: bottom - f.top, label: { x: x + 10, y: f.top + 4, w: 12 + 6.6 * `sandbox: ${f.name}`.length, h: 16 } });
  }
  const frameBottom = single ? regions.get(frames[0].name).y + regions.get(frames[0].name).h : Math.max(agentsBottom, layersBottom);

  // The tools panel.
  const host = d.toolNodes.filter((t) => t.where === "host");
  const inside = d.toolNodes.filter((t) => t.where === "sandbox");
  const tools = new Map();
  let hostBox = null;
  let sandboxBox = null;
  let ty = TOP;
  const place = (list) => {
    const first = ty + BAND;
    list.forEach((t, i) => tools.set(t.name, { x: TX + (TOOLS_W - TOOL_W) / 2, y: first + i * TOOL_PITCH, w: TOOL_W, h: NODE_H }));
    const h = BAND + list.length * TOOL_PITCH - (TOOL_PITCH - NODE_H) + PAD;
    const box = { x: TX, y: ty, w: TOOLS_W, h };
    ty += h + 20;
    return box;
  };
  if (host.length > 0) hostBox = place(host);
  if (inside.length > 0) sandboxBox = place(inside);
  const toolsBottom = d.toolNodes.length > 0 ? ty - 20 : 0;

  const bottom = Math.max(frameBottom, toolsBottom);
  const width = d.toolNodes.length > 0 ? TX + TOOLS_W + PAD : LX + LAYER_W + 2 * PAD;
  return { agents, layers, regions, tools, hostBox, sandboxBox, width, height: bottom + 44 };
}

/**
 * @param {import("./diagram.js").Diagram} d
 * @param {{ themed?: boolean }} [options]  themed: the page paints the picture from its design tokens through the dg-* classes, so no colour is written in it; without it every colour is a literal, for the downloaded file.
 * @returns {string}
 */
export function diagramToSvg(d, { themed = false } = {}) {
  const L = layoutDiagram(d);
  const out = [];
  const w = L.width;
  const h = L.height;
  // A class always; the literal colour attributes only for the file.
  const k = (cls, colours = "") => ` class="${cls}"${themed ? "" : ` ${colours}`}`;
  const font = themed ? "" : ' font-family="system-ui, -apple-system, Segoe UI, sans-serif"';
  out.push(
    `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${w} ${h}" width="${w}" height="${h}"${font} font-size="13" role="img" aria-label="${esc(d.draft ? "Architecture diagram (draft)" : "Architecture diagram")}" data-diagram="true" class="dg">`,
  );
  out.push(`<title>${esc(d.draft ? "Architecture diagram (draft, unsaved changes)" : "Architecture diagram")}</title>`);
  out.push(
    `<defs><marker id="hand" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse"><path d="M0,0 L10,5 L0,10 z"${k("dg-handoff-head", `fill="${HANDOFF}"`)}/></marker></defs>`,
  );
  out.push(`<rect width="${w}" height="${h}"${k("dg-paper", `fill="${PALETTE.paper}"`)}/>`);

  // Sandbox frames: round the agents that run in them.
  for (const r of d.regions) {
    const box = L.regions.get(r.name);
    if (!box) continue;
    out.push(
      `<g data-region="${esc(r.name)}"><title>${esc(`sandbox ${r.name}: ${r.agents.join(", ")}`)}</title>` +
        `<rect x="${box.x}" y="${box.y}" width="${box.w}" height="${box.h}" rx="12"${k("dg-region", `fill="${PALETTE.sandbox}" stroke="${MUTED}"`)} stroke-width="1.5" stroke-dasharray="7 5"/>` +
        `<text x="${box.label.x}" y="${box.y + 16}" font-size="11"${k("dg-region-title", `fill="${MUTED}"`)}>sandbox: ${esc(r.name)}</text></g>`,
    );
  }

  // The tools panel.
  const toolBox = (box, kind, title) =>
    box
      ? `<g data-tool-region="${kind}"><rect x="${box.x}" y="${box.y}" width="${box.w}" height="${box.h}" rx="12"${k(kind === "host" ? "dg-host" : "dg-region", `fill="${kind === "host" ? PALETTE.host : PALETTE.sandbox}" stroke="${MUTED}"`)} stroke-width="1.5"${kind === "host" ? "" : ' stroke-dasharray="7 5"'}/>` +
        `<text x="${box.x + 18}" y="${box.y + 20}" font-size="11" font-weight="700"${k(kind === "host" ? "dg-host-title" : "dg-region-title", `fill="${MUTED}"`)}>${title}</text></g>`
      : "";
  out.push(toolBox(L.hostBox, "host", "Harness host"), toolBox(L.sandboxBox, "sandbox", "Tools inside the sandbox"));

  // Change edges (and, for a focused agent, read edges), one guard per edge at the layer's end.
  const guardMarks = [];
  for (const e of d.edges) {
    const a = L.agents.get(e.agent);
    const l = L.layers.get(e.layer);
    if (!a || !l) continue;
    const x1 = a.x + a.w;
    const y1 = a.y + a.h / 2;
    const x2 = l.x;
    const y2 = l.y + l.h / 2;
    const mx = (x1 + x2) / 2;
    const read = e.access === "read";
    out.push(
      `<path data-edge="${esc(e.agent)}>${esc(e.layer)}" data-access="${e.access}" d="M${x1},${y1} C${mx},${y1} ${mx},${y2} ${x2},${y2}" fill="none"${k(read ? "dg-edge-read" : "dg-edge-change", `stroke="${read ? PALETTE.read : CHANGE}"`)} stroke-width="${read ? 1.2 : 1.6}"${read ? ' stroke-dasharray="3 4"' : ""}><title>${esc(`${e.agent} may ${e.access} ${e.layer}`)}</title></path>`,
    );
    const here = d.guards.filter((g) => g.agent === e.agent && g.layer === e.layer);
    if (here.length === 0) continue;
    const mode = here.some((g) => g.mode === "blocking") ? "blocking" : "warning";
    const rules = here.filter((g) => g.mode === mode).map((g) => g.rule);
    guardMarks.push({ e, mode, rules, gx: x2 - PAD, gy: y2, glyph: here.find((g) => g.mode === mode).glyph });
  }

  // Tool edges of the focused agent, routed round the layers along the lanes.
  const laneTop = 20;
  const laneBottom = h - 30;
  for (const t of d.tools) {
    const a = L.agents.get(d.focus);
    const node = L.tools.get(t.name);
    const meta = d.toolNodes.find((n) => n.name === t.name);
    if (!a || !node || !meta) continue;
    const lane = meta.where === "host" ? laneTop : laneBottom;
    const vx = a.x + a.w + 14;
    const hx = TX - 28;
    out.push(
      `<path data-tool-edge="${esc(t.name)}" d="M${a.x + a.w},${a.y + a.h / 2} H${vx} V${lane} H${hx} V${node.y + node.h / 2} H${node.x}" fill="none"${k("dg-edge-tool", `stroke="${MUTED}"`)} stroke-width="1.2" stroke-dasharray="2 4"><title>${esc(`${d.focus} calls ${t.name}`)}</title></path>`,
    );
  }

  // Hand-offs: arcs left of the agent column.
  for (const hnd of d.handoffs) {
    const a = L.agents.get(hnd.from);
    const b = L.agents.get(hnd.to);
    if (!a || !b) continue;
    const y1 = a.y + a.h / 2;
    const y2 = b.y + b.h / 2;
    const bow = AX - PAD - Math.min(60, 20 + Math.abs(y2 - y1) / 6);
    out.push(
      `<path data-handoff="${esc(hnd.from)}>${esc(hnd.to)}" d="M${a.x},${y1} C${bow},${y1} ${bow},${y2} ${b.x},${y2}" fill="none"${k("dg-handoff", `stroke="${HANDOFF}"`)} stroke-width="1.8" marker-end="url(#hand)"><title>${esc(`hand-off ${hnd.from} to ${hnd.to}: ${hnd.when}`)}</title></path>`,
    );
  }

  // Agents: the name, the layers it may change, and the colour of its slot on the left edge.
  for (const ag of d.nodes.agents) {
    const b = L.agents.get(ag.name);
    const dim = d.focus && !ag.focused;
    const slot = agentSlot(ag.name);
    const others = sandboxesOf(d, ag.name).slice(1);
    const sub = `${plural(ag.changeable, "layer", "layers")} changeable${others.length > 0 ? ` · also in ${others.join(", ")}` : ""}`;
    out.push(
      `<g data-agent="${esc(ag.name)}" tabindex="0" role="button" aria-label="Focus ${esc(ag.name)}" aria-pressed="${ag.focused}"${ag.focused ? ' data-focused="true"' : ""}${dim ? ' opacity="0.55"' : ""}>` +
        `<rect x="${b.x}" y="${b.y}" width="${b.w}" height="${b.h}" rx="8"${k(ag.focused ? "dg-agent dg-focused" : "dg-agent", `fill="${ag.focused ? PALETTE.focused : PALETTE.paper}" stroke="${ag.focused ? CHANGE : INK}"`)} stroke-width="${ag.focused ? 2 : 1}"/>` +
        `<rect x="${b.x}" y="${b.y}" width="5" height="${b.h}" rx="2" data-slot="${slot}"${k("dg-agent-edge", `fill="${PALETTE.agents[slot]}"`)}/>` +
        `<text x="${b.x + 16}" y="${b.y + 20}" font-weight="700"${k("dg-text", `fill="${INK}"`)}>${esc(ag.name)}</text>` +
        `<text x="${b.x + 16}" y="${b.y + 36}" font-size="10"${k("dg-sub", `fill="${MUTED}"`)}>${esc(sub)}</text></g>`,
    );
  }

  // Layers: the name, the pattern, and on the right how many files match.
  for (const ly of d.nodes.layers) {
    const b = L.layers.get(ly.name);
    const pattern = ly.unmatched > 0 ? `${plural(ly.unmatched, "file matches", "files match")} no layer` : ly.pattern;
    out.push(
      `<g data-layer="${esc(ly.name)}"${ly.overlap ? ' data-overlap="true"' : ""}>` +
        `<rect x="${b.x}" y="${b.y}" width="${b.w}" height="${b.h}" rx="8"${k(ly.overlap ? "dg-layer dg-attention" : "dg-layer", `fill="${PALETTE.layer}" stroke="${ly.overlap ? WARN.stroke : INK}"`)} stroke-width="${ly.overlap ? 2.2 : 1.2}"/>` +
        `<text x="${b.x + 14}" y="${b.y + 20}" font-weight="700"${k("dg-text", `fill="${INK}"`)}>${esc(ly.name)}</text>` +
        (ly.files === null || ly.files === undefined
          ? ""
          : `<text x="${b.x + b.w - 12}" y="${b.y + 20}" text-anchor="end" font-size="10" data-files="true"${k("dg-sub", `fill="${MUTED}"`)}>${plural(ly.files, "file", "files")}</text>`) +
        `<text x="${b.x + 14}" y="${b.y + 36}" font-size="10"${k(ly.unmatched > 0 ? "dg-sub dg-warn-text" : "dg-sub", `fill="${ly.unmatched > 0 ? WARN.ink : MUTED}"`)}>${esc(pattern)}</text>` +
        (ly.overlap
          ? `<rect x="${b.x + b.w - 70}" y="${b.y + 26}" width="58" height="15" rx="7"${k("dg-warn-box", `fill="${WARN.fill}" stroke="${WARN.stroke}"`)} stroke-width="1.2"/>` +
            `<text x="${b.x + b.w - 41}" y="${b.y + 37}" text-anchor="middle" font-size="10.5" font-weight="700"${k("dg-warn-text", `fill="${WARN.ink}"`)}>overlap</text>`
          : "") +
        `</g>`,
    );
  }

  // Tools: a name and what it is and who it is for.
  for (const t of d.toolNodes) {
    const b = L.tools.get(t.name);
    const on = !d.focus || d.tools.some((x) => x.name === t.name);
    out.push(
      `<g data-tool="${esc(t.name)}"${on ? "" : ' opacity="0.35"'}>` +
        `<rect x="${b.x}" y="${b.y}" width="${b.w}" height="${b.h}" rx="8"${k("dg-agent", `fill="${PALETTE.paper}" stroke="${INK}"`)} stroke-width="1"/>` +
        `<text x="${b.x + 14}" y="${b.y + 20}" font-weight="700"${k("dg-text", `fill="${INK}"`)}>${esc(t.name)}</text>` +
        `<text x="${b.x + 14}" y="${b.y + 36}" font-size="10"${k("dg-sub", `fill="${MUTED}"`)}>${esc(`${t.kindText} · ${t.users}`)}</text></g>`,
    );
  }

  // Guards on top of the edges: the glyph says blocking or warning without colour.
  for (const g of guardMarks) {
    const c = g.mode === "blocking" ? BLOCK : WARN;
    const tone = g.mode === "blocking" ? "block" : "warn";
    out.push(
      `<g data-guard="${esc(g.rules.join(" "))}" data-mode="${g.mode}" data-guard-agent="${esc(g.e.agent)}" data-guard-layer="${esc(g.e.layer)}"><title>${esc(`${g.mode === "blocking" ? "blocking" : "warning"}: ${g.rules.join(", ")}`)}</title>` +
        `<circle cx="${num(g.gx)}" cy="${num(g.gy)}" r="9"${k(`dg-guard-${tone}`, `fill="${c.fill}" stroke="${c.stroke}"`)} stroke-width="2"/>` +
        `<text x="${num(g.gx)}" y="${num(g.gy + 3.5)}" text-anchor="middle" font-size="11" font-weight="700"${k(`dg-glyph-${tone}`, `fill="${c.ink}"`)}>${g.glyph}</text></g>`,
    );
  }

  if (d.draft) {
    out.push(
      `<g data-draft="true"><rect x="${w - 168}" y="6" width="152" height="24" rx="12"${k("dg-warn-box", `fill="${WARN.fill}" stroke="${WARN.stroke}"`)}/>` +
        `<text x="${w - 92}" y="22" text-anchor="middle" font-weight="600"${k("dg-warn-text", `fill="${WARN.ink}"`)}>Draft · not saved</text></g>`,
    );
  }
  out.push(
    `<text x="${AX - PAD}" y="${h - 14}" font-size="10"${k("dg-sub", `fill="${MUTED}"`)}>solid line: may change · dashed line: may read · ✕ blocking hook · ! warning hook · dashed box: sandbox</text>`,
  );
  out.push("</svg>");
  return out.join("\n");
}
