// The search over the Context tab: one query, matched case-insensitively against a row's fields.

export const normalize = (query: string): string => query.trim().toLowerCase();

/** The row matches when no query is set, or when any of its fields contains it. */
export const matches = (query: string, ...fields: readonly string[]): boolean => query === "" || fields.some((f) => f.toLowerCase().includes(query));

/** "5" without a query; "2 of 5" while one filters the rows. */
export const countLabel = (query: string, shown: number, total: number): string => (query === "" ? String(total) : `${shown} of ${total}`);
