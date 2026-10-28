// Telling a pasted token from a word (UX spec section 5): the MCP form warns
// about one, and the agent detail masks one that sits in a server's URL or
// arguments. A secret value is never shown; HIDDEN stands in for it.

export const HIDDEN = "••••••";

const TOKENS = [
  ["GitHub token", /^(?:gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,})$/],
  ["Linear API key", /^lin_(?:api|oauth)_[A-Za-z0-9]{16,}$/],
  ["API key", /^sk-[A-Za-z0-9_-]{16,}$/],
  ["Slack token", /^xox[abprs]-[A-Za-z0-9-]{10,}$/],
  ["AWS access key", /^AKIA[0-9A-Z]{16}$/],
  ["JWT", /^eyJ[A-Za-z0-9_-]{5,}\.eyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}$/],
];

/**
 * Whether a value is a token someone pasted, and which kind. An auth scheme
 * ("Bearer ") or a flag ("--key=") in front is skipped; only the token is the
 * secret. A reference (${VAR}) and ordinary words are not tokens.
 * @param {string} value
 * @returns {{ kind: string, secret: string } | null}
 */
export function detectToken(value) {
  if (typeof value !== "string" || value === "" || value.includes("${")) return null;
  const candidate = /^(?:(?:Bearer|Basic|Token)\s+|--?[\w-]+=)?(\S+)$/i.exec(value.trim())?.[1];
  if (!candidate) return null;
  for (const [kind, re] of TOKENS) if (re.test(candidate)) return { kind, secret: candidate };
  if (/^[A-Za-z0-9_=-]{32,}$/.test(candidate) && /[0-9]/.test(candidate) && /[A-Za-z]/.test(candidate)) return { kind: "long secret", secret: candidate };
  return null;
}

