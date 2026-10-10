// Zones and bounded contexts as the code views show them. The server owns the
// membership rule (contract §6); this only names and colours what it sends.
// A context's hue is its index in CodeMap.contexts mod 8, so one context has
// one hue on the map, in tags and in the tree.

import { formatCount } from "./code.js";

export const HUES = 8;

/**
 * @typedef {{ id: string, name: string, hue: string, contextName: string }} ZoneInfo
 *   hue: "0"…"7", or "none" for a zone in no bounded context
 */

/**
 * Every zone of a map, by id, with its context's hue.
 * @param {import("./code.js").CodeMap|null} map
 * @returns {Map<string, ZoneInfo>}
 */
export function zoneIndex(map) {
  const index = new Map();
  if (!map) return index;
  map.contexts.forEach((ctx, i) => {
    for (const z of ctx.zones) {
      if (!index.has(z.id)) index.set(z.id, { id: z.id, name: z.name, hue: String(i % HUES), contextName: ctx.name });
    }
  });
  for (const z of map.unassignedZones) {
    if (!index.has(z.id)) index.set(z.id, { id: z.id, name: z.name, hue: "none", contextName: "" });
  }
  return index;
}

/** The hue of a context by its place in the map. */
export const contextHue = (i) => String(i % HUES);

/** A zone tag's title: what the swatch means in words. */
export const zoneTitle = (z) => (z.contextName ? `Zone · ${z.contextName} context` : "Zone · no bounded context");

/**
 * A file head's zone tags: up to two, then "+N" with the rest named in its title.
 * Zones the map does not know are left out.
 * @param {string[]} ids
 * @param {Map<string, ZoneInfo>} index
 * @returns {{ tags: (ZoneInfo & { title: string })[], more: { label: string, title: string } | null }}
 */
export function zoneTags(ids, index) {
  const known = (ids ?? []).map((id) => index.get(id)).filter(Boolean);
  const tags = known.slice(0, 2).map((z) => ({ ...z, title: zoneTitle(z) }));
  const rest = known.slice(2);
  return { tags, more: rest.length ? { label: `+${rest.length}`, title: rest.map((z) => z.name).join(", ") } : null };
}

/** The tree's dot for a file: its first known zone (UX §5), or null. */
export function zoneDot(ids, index) {
  const z = (ids ?? []).map((id) => index.get(id)).find(Boolean);
  return z ? { hue: z.hue, label: z.contextName ? `${z.name}, ${z.contextName}` : z.name } : null;
}

/**
 * The longest directory a pattern names literally, where a zone link opens
 * Files: "internal/adapter/out/git/**" → "internal/adapter/out/git".
 * A pattern whose first segment is a wildcard opens the root ("").
 */
export function literalDir(pattern) {
  const segments = (pattern ?? "").replace(/^\.?\//, "").split("/");
  const out = [];
  for (const s of segments) {
    if (s === "" || /[*?[\]{}!]/.test(s)) break;
    out.push(s);
  }
  // A last literal segment with a dot is most likely a file: open its directory.
  if (out.length === segments.length && out.length > 0 && out.at(-1).includes(".")) out.pop();
  return out.join("/");
}

/**
 * The map as its blocks draw it: the overview's segments, one card per
 * context (then "No bounded context", then "Unzoned files"), zone meters
 * scaled to the largest zone on the map.
 * @param {import("./code.js").CodeMap} map
 */
export function mapView(map) {
  const allZones = [...map.contexts.flatMap((c) => c.zones), ...map.unassignedZones];
  const largest = Math.max(1, ...allZones.map((z) => z.fileCount));
  const zoneRow = (z) => ({
    id: z.id,
    name: z.name,
    pattern: z.pattern,
    count: z.fileCount === 0 ? "no files" : formatCount(z.fileCount),
    empty: z.fileCount === 0,
    share: z.fileCount / largest,
    dir: literalDir(z.pattern),
  });
  const cards = map.contexts.map((c, i) => ({
    key: `ctx-${c.id}`,
    kind: "",
    hue: contextHue(i),
    name: c.name,
    purpose: c.purpose,
    count: `${formatCount(c.fileCount)} ${c.fileCount === 1 ? "file" : "files"}`,
    zones: c.zones.map(zoneRow),
  }));
  if (map.unassignedZones.length) {
    const n = map.unassignedZones.reduce((s, z) => s + z.fileCount, 0);
    cards.push({ key: "unassigned", kind: "unassigned", hue: "none", name: "No bounded context", purpose: "", count: `${formatCount(n)} ${n === 1 ? "file" : "files"}`, zones: map.unassignedZones.map(zoneRow) });
  }
  if (map.unzonedFileCount > 0) {
    cards.push({ key: "unzoned", kind: "unzoned", hue: "none", name: "Unzoned files", purpose: "", count: formatCount(map.unzonedFileCount), zones: [] });
  }
  const spread = map.contexts.map((c, i) => ({ key: `s-${c.id}`, hue: contextHue(i), kind: "", share: c.fileCount }));
  if (map.unassignedZones.length) spread.push({ key: "s-unassigned", hue: "none", kind: "", share: map.unassignedZones.reduce((s, z) => s + z.fileCount, 0) });
  if (map.unzonedFileCount > 0) spread.push({ key: "s-unzoned", hue: "none", kind: "unzoned", share: map.unzonedFileCount });
  const zoneCount = allZones.length;
  return {
    empty: zoneCount === 0,
    totals: `${formatCount(map.fileCount)} ${map.fileCount === 1 ? "file" : "files"} · ${formatCount(map.contexts.length)} ${map.contexts.length === 1 ? "context" : "contexts"} · ${formatCount(zoneCount)} ${zoneCount === 1 ? "zone" : "zones"} · ${formatCount(map.unzonedFileCount)} unzoned`,
    spread: spread.filter((s) => s.share > 0),
    cards,
  };
}
