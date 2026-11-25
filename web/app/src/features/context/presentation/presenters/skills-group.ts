import type { Skill, SkillSource, Usage } from "../../domain/session-context";
import type { HeadingBase, HookRow, HooksGroup, SkillRow, SkillsGroup } from "./context-view-model";
import { countLabel, matches } from "./search";

const SOURCE_LABEL: Record<SkillSource, string> = { mode: "Mode", repository: "Repository", agent: "Agent", built_in: "Built in" };

/**
 * The names in the order the tab shows them: the skills invoked when the tab
 * opened first (most used first), then the rest as the entry lists them. The
 * order is kept for as long as the tab is open, so a later invocation changes
 * a count and never moves a row.
 */
export function skillOrder(skills: readonly Skill[], usage: Usage): string[] {
  const used = (s: Skill) => usage.skills.get(s.name)?.count ?? 0;
  const invoked = skills.filter((s) => used(s) > 0).sort((a, b) => used(b) - used(a) || a.name.localeCompare(b.name));
  return [...invoked, ...skills.filter((s) => used(s) === 0)].map((s) => s.name);
}

export function skillsGroup(heading: HeadingBase, skills: readonly Skill[], order: readonly string[], usage: Usage, query: string, rings: ReadonlySet<string>): SkillsGroup {
  const byName = new Map(skills.map((s) => [s.name, s]));
  const rank = new Map(order.map((name, i) => [name, i]));
  // A skill the order has not seen (none today) goes last.
  const sorted = [...byName.values()].sort((a, b) => (rank.get(a.name) ?? order.length) - (rank.get(b.name) ?? order.length));
  const rows = sorted.map((s): SkillRow => {
    const use = usage.skills.get(s.name);
    return { name: s.name, source: SOURCE_LABEL[s.source], description: s.description, invoked: use ? `invoked ×${use.count}` : "", ringing: rings.has(`skill:${s.name}`) };
  });
  const shown = rows.filter((r) => matches(query, r.name, r.source, r.description));
  return { ...heading, count: countLabel(query, shown.length, rows.length), noMatch: query !== "" && shown.length === 0, rows: shown };
}

export function hooksGroup(heading: HeadingBase, hooks: readonly { event: string; matcher: string; rule: string }[], query: string): HooksGroup {
  const rows = hooks.map((h): HookRow => ({ event: h.event, matcher: h.matcher, rule: h.rule }));
  const shown = rows.filter((r) => matches(query, r.event, r.matcher, r.rule));
  return { ...heading, count: countLabel(query, shown.length, rows.length), noMatch: query !== "" && shown.length === 0, rows: shown };
}
