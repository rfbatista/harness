// A 16px line icon, decorative: the words beside it carry the meaning. The
// same drawings as the templ top bar.

export type IconName = "key" | "saver" | "model" | "shield" | "server" | "bell";

const PATHS: Record<IconName, string> = {
  key: "M5.5 6.75a3.25 3.25 0 1 1 3.1 3.25L7.25 11.5H5.75V13H4.25v1.25h-2.5v-2.5l3.9-3.9A3.2 3.2 0 0 1 5.5 6.75z M10.5 5.1a.6.6 0 1 1 0 1.2.6.6 0 0 1 0-1.2z",
  saver: "M9 1.75 3.5 9h4L7 14.25 12.5 7h-4z",
  model: "M4.75 4.75h6.5v6.5h-6.5z M6.5 1.75v3 M9.5 1.75v3 M6.5 11.25v3 M9.5 11.25v3 M1.75 6.5h3 M1.75 9.5h3 M11.25 6.5h3 M11.25 9.5h3",
  shield: "M8 1.75 2.75 3.5v4.25c0 3 2.25 5 5.25 6.5 3-1.5 5.25-3.5 5.25-6.5V3.5z",
  server: "M2.25 2.25h11.5v4.5H2.25z M2.25 9.25h11.5v4.5H2.25z M4.75 4.5h.5 M4.75 11.5h.5",
  bell: "M4 11.25V7a4 4 0 0 1 8 0v4.25l1.25 1.25H2.75z M6.5 13.75a1.6 1.6 0 0 0 3 0",
};

export function Icon({ name }: { name: IconName }) {
  return (
    <svg aria-hidden="true" viewBox="0 0 16 16">
      <path d={PATHS[name]} fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinejoin="round" strokeLinecap={name === "model" || name === "key" || name === "bell" || name === "server" ? "round" : undefined} />
    </svg>
  );
}
