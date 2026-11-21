/** The badge stops counting here (ADR 0096). */
const BADGE_MAX = 99;

/** The badge's text: "", "3", "99+". */
export function badgeText(count: number | null): string {
  if (!count) return "";
  return count > BADGE_MAX ? `${BADGE_MAX}+` : String(count);
}

/** The bell's accessible name, with the exact count. */
export function bellLabel(count: number | null): string {
  if (!count) return "Notifications";
  return `Notifications, ${count} unread`;
}
