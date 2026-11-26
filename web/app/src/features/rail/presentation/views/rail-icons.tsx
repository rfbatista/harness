// The rail's icons: the same drawings as the templ shell (shell/icons.templ),
// decorative: the words beside them carry the meaning.

export type RailIconName = "board" | "code" | "services" | "review" | "document" | "design" | "design-system" | "usage" | "owner" | "plus" | "close" | "chevron" | "rail";

const PATHS: Record<Exclude<RailIconName, "chevron">, { d: string; round?: boolean; join?: boolean }> = {
  board: { d: "M2 2.75h12v10.5H2z M6 2.75v10.5 M10 2.75v10.5", join: true },
  code: { d: "M5.5 4.5 2 8l3.5 3.5 M10.5 4.5 14 8l-3.5 3.5 M9 3 7 13", join: true, round: true },
  document: { d: "M3.75 1.75h5.5L12.25 4.75v9.5h-8.5z M9 1.75V5h3.25 M6 8.25h4 M6 11h4", join: true },
  design: { d: "M2 2.25h5v5H2z M11.5 2.1a2.6 2.6 0 1 1 0 5.2 2.6 2.6 0 0 1 0-5.2z M4.5 9.25 7.25 13.75h-5.5z M9.25 9.25h4.75v4.5H9.25z", join: true },
  "design-system": { d: "M2.25 2.25h4.5v4.5h-4.5z M9.25 2.25h4.5v4.5h-4.5z M2.25 9.25h4.5v4.5h-4.5z M11.5 9.1a2.4 2.4 0 1 1 0 4.8 2.4 2.4 0 0 1 0-4.8z", join: true },
  review: { d: "M2 3h12v7.75H8.5L5 13.5v-2.75H2z M5.5 6.9l1.75 1.6L10.5 5.4", join: true, round: true },
  usage: { d: "M2 13.75h12 M4 11.5V8 M7 11.5V4 M10 11.5V6.5 M13 11.5V2.5", round: true },
  owner: { d: "M2.5 3.5h11v7.25H7L4 13v-2.25H2.5z M5.25 6h5.5 M5.25 8.25h3.5", join: true, round: true },
  plus: { d: "M8 3v10M3 8h10", round: true },
  close: { d: "M4 4l8 8M12 4l-8 8", round: true },
  rail: { d: "M2 2.75h12v10.5H2z M6 2.75v10.5", join: true },
  services: { d: "M2.5 2.75h11v4h-11z M2.5 9.25h11v4h-11z M5 4.75h.01 M5 11.25h.01 M8 4.75h4 M8 11.25h4", join: true, round: true },
};

export function RailIcon({ name }: { name: RailIconName }) {
  if (name === "chevron") {
    return (
      <svg aria-hidden="true" viewBox="0 0 12 12">
        <path d="M3 4.5 6 7.5 9 4.5" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
    );
  }
  const p = PATHS[name];
  return (
    <svg aria-hidden="true" viewBox="0 0 16 16">
      <path d={p.d} fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinejoin={p.join ? "round" : undefined} strokeLinecap={p.round ? "round" : undefined} />
    </svg>
  );
}

const GLYPH = { className: "[ glyph ]", "aria-hidden": true, viewBox: "0 0 16 16", fill: "none", stroke: "currentColor", strokeWidth: 1.5, strokeLinecap: "round", strokeLinejoin: "round" } as const;

/** The merge glyphs of a row: CSS shows the one that matches the row's data-merge-state. */
export function MergeGlyphs() {
  return (
    <>
      <svg {...GLYPH} data-glyph="merged">
        <circle cx="8" cy="8" r="6.25" />
        <path d="M5.3 8.2l1.8 1.8 3.6-3.8" />
      </svg>
      <svg {...GLYPH} data-glyph="partial">
        <circle cx="8" cy="8" r="6.25" />
        <path d="M8 1.75a6.25 6.25 0 0 1 0 12.5z" fill="currentColor" stroke="none" />
      </svg>
      <svg {...GLYPH} data-glyph="not_merged">
        <circle cx="8" cy="8" r="6.25" />
      </svg>
      <svg {...GLYPH} data-glyph="unknown">
        <circle cx="8" cy="8" r="6.25" strokeDasharray="2.4 2.2" />
        <path d="M6.4 6.3a1.7 1.7 0 1 1 2.3 1.6c-.5.2-.7.6-.7 1.1v.3" />
        <circle cx="8" cy="11.3" r=".55" fill="currentColor" stroke="none" />
      </svg>
    </>
  );
}
