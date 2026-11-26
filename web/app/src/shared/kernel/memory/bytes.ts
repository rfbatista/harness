// Memory as the person reads it: 0.1 GB from 1 GB up, whole MB below. Binary units, named GB and MB as the UI does.

const MB = 1024 ** 2;
const GB = 1024 ** 3;

/** "3.4 GB", "905 MB". */
export function formatShown(bytes: number): string {
  // MB until its rounding would reach 1024: that is 1.0 GB, never "1024 MB".
  if (Math.round(bytes / MB) < 1024) return `${Math.round(bytes / MB)} MB`;
  return `${(Math.round((bytes / GB) * 10 + 1e-9) / 10).toFixed(1)} GB`;
}

/** The orçamento as the person set it: "4 GB", "2.5 GB", "512 MB". */
export function formatBudget(bytes: number): string {
  return formatShown(bytes).replace(".0 GB", " GB");
}

/** The number and the unit apart, so the unit can sit in the muted "of" part: ["3.4", "GB"]. */
export function splitShown(bytes: number): [string, string] {
  const [number = "", unit = ""] = formatShown(bytes).split(" ");
  return [number, unit];
}
