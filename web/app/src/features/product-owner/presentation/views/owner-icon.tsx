// The product owner's line icons, drawn on a 20-unit grid as the design study's sprite has them.
// Decorative: the words beside them, or the button's accessible name, carry the meaning.

export type OwnerIconName = "owner" | "plus" | "history" | "info" | "expand" | "collapse" | "close" | "send" | "stop" | "back" | "gear" | "search" | "warn" | "tasks";

const PATHS: Record<OwnerIconName, readonly string[]> = {
  owner: ["M4 4.5h12a1 1 0 0 1 1 1v7a1 1 0 0 1-1 1H9l-4 3v-3H4a1 1 0 0 1-1-1v-7a1 1 0 0 1 1-1Z", "M7 8h6M7 10.5h4"],
  plus: ["M10 4v12M4 10h12"],
  history: ["M3.5 10a6.5 6.5 0 1 0 1.9-4.6", "M3.5 3.5v3.3h3.3", "M10 6.5V10l2.5 1.5"],
  info: ["M10 3a7 7 0 1 0 0 14 7 7 0 0 0 0-14Z", "M10 9v5M10 6.5v.2"],
  expand: ["M11.5 3.5H16.5v5M8.5 16.5H3.5v-5M16.5 3.5 11 9M3.5 16.5 9 11"],
  collapse: ["M8.5 3.5v5h-5M11.5 16.5v-5h5M3.5 3.5 8.5 8.5M16.5 16.5 11.5 11.5"],
  close: ["M5 5l10 10M15 5 5 15"],
  send: ["M10 16V4M4.5 9.5 10 4l5.5 5.5"],
  stop: ["M7 5.5h6a1.5 1.5 0 0 1 1.5 1.5v6a1.5 1.5 0 0 1-1.5 1.5H7A1.5 1.5 0 0 1 5.5 13V7A1.5 1.5 0 0 1 7 5.5Z"],
  back: ["M12.5 5 7.5 10l5 5"],
  gear: ["M10 7.5a2.5 2.5 0 1 0 0 5 2.5 2.5 0 0 0 0-5Z", "M10 2.5v2M10 15.5v2M2.5 10h2M15.5 10h2M4.7 4.7l1.4 1.4M13.9 13.9l1.4 1.4M4.7 15.3l1.4-1.4M13.9 6.1l1.4-1.4"],
  warn: ["M10 3.5 17 16H3z", "M10 8v4M10 14.2v.2"],
  tasks: ["M4 6h12M4 10h12M4 14h8"],
  search: ["M8.5 3.5a5 5 0 1 0 0 10 5 5 0 0 0 0-10Z", "m12.5 12.5 4 4"],
};

export function OwnerIcon({ name }: { name: OwnerIconName }) {
  return (
    <svg className="[ icon ]" aria-hidden="true" viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
      {PATHS[name].map((d) => (
        <path key={d} d={d} />
      ))}
    </svg>
  );
}

/** The product owner's mark: the glyph everywhere it acts or speaks. */
export function OwnerMark({ size, off }: { size?: "sm" | "lg"; off?: boolean }) {
  return (
    <span className="[ po-mark ]" data-size={size} data-off={off ? "" : undefined} aria-hidden="true">
      <OwnerIcon name="owner" />
    </span>
  );
}
