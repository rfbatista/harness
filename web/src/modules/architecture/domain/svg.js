// A Diagram model (domain/diagram.js) as standalone SVG text. Pure: it lays
// the model out itself (agents in a column, layers in another, sandboxes as
// regions around the agents that run in them) and draws it with inline
// attributes only, so the same text is the picture on the page and the file
// the person downloads. Elements carry data attributes (data-agent,
// data-layer, data-edge, data-guard, data-region) for the page to hook onto.

const AGENT_W = 190;
const NODE_H = 34;
const AGENT_PITCH = 48;
const REGION_GAP = 30;
const LAYER_W = 250;
const LAYER_H = 44;
const LAYER_PITCH = 62;
const LEFT = 96; // room for hand-off arcs, then the region padding
const GAP = 280; // between the agent and layer columns
const TOP = 40;

// The picture's colours as literals, on purpose: the same text is downloaded as
// a standalone SVG file, which cannot read the page's CSS variables (no design
// tokens outside the page), so every colour is gathered here and nowhere else.
const PALETTE = {
  ink: "#1f2937",
  muted: "#6b7280",
  change: "#2563eb",
  read: "#94a3b8",
  paper: "#ffffff",
  sandbox: "#f4f6fa",
  layer: "#f8fafc",
  focused: "#dbeafe",
  block: { fill: "#fde8e8", stroke: "#b42318", ink: "#7a1d14" },
  warn: { fill: "#fff1cc", stroke: "#b54708", ink: "#7a3b05" },
};
const HANDOFF = "#7c3aed"; // hand-off arrows: their own hue, neither change nor read
const { ink: INK, muted: MUTED, change: CHANGE, block: BLOCK, warn: WARN } = PALETTE;

const esc = (s) => String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
const num = (n) => Math.round(n * 10) / 10;

/** The room a diagram takes and where each node sits; exported for the page's tests. */
export function layoutDiagram(d) {
  const region = (name) => d.regions.findIndex((r) => r.agents.includes(name));
  const agents = [...d.nodes.agents].sort((a, b) => {
    const ra = region(a.name);
    const rb = region(b.name);
    return (ra < 0 ? Infinity : ra) - (rb < 0 ? Infinity : rb);
  });
  const at = new Map();
  let y = TOP;
  let prev = null;
  for (const a of agents) {
    const r = region(a.name);
    if (prev !== null && r !== prev) y += REGION_GAP;
    else if (prev === null && r >= 0) y += REGION_GAP - 10;
    at.set(a.name, { x: LEFT, y, w: AGENT_W, h: NODE_H });
    y += AGENT_PITCH;
    prev = r;
  }
  const agentsBottom = y;
  const lx = LEFT + AGENT_W + GAP;
  const layers = new Map(d.nodes.layers.map((l, i) => [l.name, { x: lx, y: TOP + i * LAYER_PITCH, w: LAYER_W, h: LAYER_H }]));
  const layersBottom = TOP + d.nodes.layers.length * LAYER_PITCH;
  return { agents: at, layers, width: lx + LAYER_W + 40, height: Math.max(agentsBottom, layersBottom) + 56 };
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

  // Sandboxes: a region around the agents that run in it.
  d.regions.forEach((r) => {
    const boxes = r.agents.map((a) => L.agents.get(a)).filter(Boolean);
    if (boxes.length === 0) return;
    const top = Math.min(...boxes.map((b) => b.y));
    const bottom = Math.max(...boxes.map((b) => b.y + b.h));
    out.push(
      `<g data-region="${esc(r.name)}"><title>${esc(`sandbox ${r.name}: ${r.agents.join(", ")}`)}</title>` +
        `<rect x="${LEFT - 14}" y="${top - 26}" width="${AGENT_W + 28}" height="${bottom - top + 36}" rx="12"${k("dg-region", `fill="${PALETTE.sandbox}" stroke="${MUTED}"`)} stroke-width="1.2" stroke-dasharray="6 4"/>` +
        `<text x="${LEFT - 4}" y="${top - 10}" font-size="11"${k("dg-region-title", `fill="${MUTED}"`)}>sandbox: ${esc(r.name)}</text></g>`,
    );
  });

  // Change edges (and, for a focused agent, read edges), then their guards.
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
      `<path data-edge="${esc(e.agent)}>${esc(e.layer)}" data-access="${e.access}" d="M${x1},${y1} C${mx},${y1} ${mx},${y2} ${x2},${y2}" fill="none"${k(read ? "dg-edge-read" : "dg-edge-change", `stroke="${read ? PALETTE.read : CHANGE}"`)} stroke-width="${read ? 1.5 : 2}"${read ? ' stroke-dasharray="6 4"' : ""}><title>${esc(`${e.agent} may ${e.access} ${e.layer}`)}</title></path>`,
    );
    const here = d.guards.filter((g) => g.agent === e.agent && g.layer === e.layer);
    const modes = ["blocking", "warning"].filter((m) => here.some((g) => g.mode === m));
    modes.forEach((mode, i) => {
      const rules = here.filter((g) => g.mode === mode).map((g) => g.rule);
      const gx = (x1 + x2) / 2 + (i - (modes.length - 1) / 2) * 24;
      const gy = (y1 + y2) / 2;
      guardMarks.push({ e, mode, rules, gx, gy, glyph: here.find((g) => g.mode === mode).glyph });
    });
  }

  // Hand-offs: arcs left of the agent column.
  for (const hnd of d.handoffs) {
    const a = L.agents.get(hnd.from);
    const b = L.agents.get(hnd.to);
    if (!a || !b) continue;
    const y1 = a.y + a.h / 2;
    const y2 = b.y + b.h / 2;
    const bow = LEFT - 14 - Math.min(60, 20 + Math.abs(y2 - y1) / 6);
    out.push(
      `<path data-handoff="${esc(hnd.from)}>${esc(hnd.to)}" d="M${a.x},${y1} C${bow},${y1} ${bow},${y2} ${b.x},${y2}" fill="none"${k("dg-handoff", `stroke="${HANDOFF}"`)} stroke-width="1.5" marker-end="url(#hand)"><title>${esc(`hand-off ${hnd.from} to ${hnd.to}: ${hnd.when}`)}</title></path>`,
    );
  }

  // Nodes.
  for (const ag of d.nodes.agents) {
    const b = L.agents.get(ag.name);
    const dim = d.focus && !ag.focused;
    out.push(
      `<g data-agent="${esc(ag.name)}" tabindex="0" role="button" aria-label="Focus ${esc(ag.name)}" aria-pressed="${ag.focused}"${ag.focused ? ' data-focused="true"' : ""}${dim ? ' opacity="0.55"' : ""}>` +
        `<rect x="${b.x}" y="${b.y}" width="${b.w}" height="${b.h}" rx="8"${k(ag.focused ? "dg-agent dg-focused" : "dg-agent", `fill="${ag.focused ? PALETTE.focused : PALETTE.paper}" stroke="${ag.focused ? CHANGE : INK}"`)} stroke-width="${ag.focused ? 2.5 : 1.2}"/>` +
        `<text x="${b.x + b.w / 2}" y="${b.y + b.h / 2 + 4.5}" text-anchor="middle"${k("dg-text", `fill="${INK}"`)}>${esc(ag.name)}</text></g>`,
    );
  }
  for (const ly of d.nodes.layers) {
    const b = L.layers.get(ly.name);
    const pattern = ly.unmatched > 0 ? `${ly.unmatched} ${ly.unmatched === 1 ? "file matches" : "files match"} no layer` : ly.pattern;
    out.push(
      `<g data-layer="${esc(ly.name)}"${ly.overlap ? ' data-overlap="true"' : ""}>` +
        `<rect x="${b.x}" y="${b.y}" width="${b.w}" height="${b.h}" rx="8"${k(ly.overlap ? "dg-layer dg-attention" : "dg-layer", `fill="${PALETTE.layer}" stroke="${ly.overlap ? WARN.stroke : INK}"`)} stroke-width="${ly.overlap ? 2.2 : 1.2}"/>` +
        `<text x="${b.x + 14}" y="${b.y + 19}" font-weight="600"${k("dg-text", `fill="${INK}"`)}>${esc(ly.name)}</text>` +
        `<text x="${b.x + 14}" y="${b.y + 35}" font-size="11"${k(ly.unmatched > 0 ? "dg-sub dg-warn-text" : "dg-sub", `fill="${ly.unmatched > 0 ? WARN.ink : MUTED}"`)}>${esc(pattern)}</text>` +
        (ly.overlap
          ? `<rect x="${b.x + b.w - 70}" y="${b.y + 26}" width="58" height="15" rx="7"${k("dg-warn-box", `fill="${WARN.fill}" stroke="${WARN.stroke}"`)} stroke-width="1.2"/>` +
            `<text x="${b.x + b.w - 41}" y="${b.y + 37}" text-anchor="middle" font-size="10.5" font-weight="700"${k("dg-warn-text", `fill="${WARN.ink}"`)}>overlap</text>`
          : "") +
        `</g>`,
    );
  }

  // Guards on top of the edges: the glyph says blocking or warning without colour.
  for (const g of guardMarks) {
    const c = g.mode === "blocking" ? BLOCK : WARN;
    const tone = g.mode === "blocking" ? "block" : "warn";
    out.push(
      `<g data-guard="${esc(g.rules.join(" "))}" data-mode="${g.mode}" data-guard-agent="${esc(g.e.agent)}" data-guard-layer="${esc(g.e.layer)}"><title>${esc(`${g.mode === "blocking" ? "blocking" : "warning"}: ${g.rules.join(", ")}`)}</title>` +
        `<circle cx="${num(g.gx)}" cy="${num(g.gy)}" r="10"${k(`dg-guard-${tone}`, `fill="${c.fill}" stroke="${c.stroke}"`)} stroke-width="1.5"/>` +
        `<text x="${num(g.gx)}" y="${num(g.gy + 4.5)}" text-anchor="middle" font-size="13" font-weight="700"${k(`dg-glyph-${tone}`, `fill="${c.ink}"`)}>${g.glyph}</text></g>`,
    );
  }

  if (d.draft) {
    out.push(
      `<g data-draft="true"><rect x="${w - 168}" y="10" width="152" height="24" rx="12"${k("dg-warn-box", `fill="${WARN.fill}" stroke="${WARN.stroke}"`)}/>` +
        `<text x="${w - 92}" y="26" text-anchor="middle" font-weight="600"${k("dg-warn-text", `fill="${WARN.ink}"`)}>Draft · not saved</text></g>`,
    );
  }
  out.push(
    `<text x="${LEFT - 14}" y="${h - 18}" font-size="11"${k("dg-sub", `fill="${MUTED}"`)}>solid line: may change · dashed line: may read · ✕ blocking hook · ! warning hook · dashed box: sandbox</text>`,
  );
  out.push("</svg>");
  return out.join("\n");
}
