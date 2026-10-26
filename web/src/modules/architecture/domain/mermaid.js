// A Diagram model as Mermaid flowchart text, to paste into a README or a
// pull request. Names are escaped; nodes get generated ids.

const escape = (s) => String(s).replace(/"/g, "#quot;").replace(/[[\]{}()<>]/g, (c) => `#${c.charCodeAt(0)};`);

/** @param {import("./diagram.js").Diagram} d */
export function diagramToMermaid(d) {
  const lines = ["flowchart LR"];
  if (d.draft) lines.push("  %% Draft: unsaved changes");
  const layerId = new Map(d.nodes.layers.map((l, i) => [l.name, `L${i}`]));
  const agentId = new Map(d.nodes.agents.map((a, i) => [a.name, `A${i}`]));

  for (const l of d.nodes.layers) lines.push(`  ${layerId.get(l.name)}["${escape(l.name)}"]`);
  const inRegion = new Set();
  d.regions.forEach((r, i) => {
    lines.push(`  subgraph S${i}["sandbox: ${escape(r.name)}"]`);
    for (const a of r.agents) {
      lines.push(`    ${agentId.get(a)}["${escape(a)}"]`);
      inRegion.add(a);
    }
    lines.push("  end");
  });
  for (const a of d.nodes.agents) if (!inRegion.has(a.name)) lines.push(`  ${agentId.get(a.name)}["${escape(a.name)}"]`);

  for (const e of d.edges) {
    const guards = d.guards.filter((g) => g.agent === e.agent && g.layer === e.layer).map((g) => `${g.glyph} ${g.rule}`);
    const label = [e.access, ...guards].map(escape).join("<br/>");
    lines.push(`  ${agentId.get(e.agent)} ${e.access === "read" ? "-.->" : "-->"}|"${label}"| ${layerId.get(e.layer)}`);
  }
  for (const h of d.handoffs) {
    if (agentId.has(h.from) && agentId.has(h.to)) lines.push(`  ${agentId.get(h.from)} ==>|"hand-off"| ${agentId.get(h.to)}`);
  }
  return lines.join("\n") + "\n";
}
