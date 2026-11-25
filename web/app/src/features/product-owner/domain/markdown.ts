// The small part of Markdown that an answer uses, as a tree. Raw HTML is text, a link goes only
// to http(s) or to a page of this site, and an unfinished marker stays as typed (an answer
// that is still arriving may end in one). A link to a task page is a task reference.

export type Inline =
  | { readonly kind: "text"; readonly text: string }
  | { readonly kind: "strong"; readonly inline: readonly Inline[] }
  | { readonly kind: "emphasis"; readonly inline: readonly Inline[] }
  | { readonly kind: "code"; readonly text: string }
  | { readonly kind: "link"; readonly href: string; readonly inline: readonly Inline[] }
  | { readonly kind: "task"; readonly taskId: string; readonly href: string; readonly title: string };

export type Block =
  | { readonly kind: "paragraph"; readonly inline: readonly Inline[] }
  | { readonly kind: "heading"; readonly inline: readonly Inline[] }
  | { readonly kind: "list"; readonly ordered: boolean; readonly items: readonly (readonly Inline[])[] }
  | { readonly kind: "fence"; readonly text: string };

const TASK_PAGE = /^\/projects\/[^/]+\/tasks\/([^/?#]+)$/;

// Links resolve against a stand-in origin: a path stays on the page's own origin exactly when it stays on this one,
// whatever the page's real origin is (so this code reads no browser global).
const SITE = "https://site.invalid";

/** The address a link goes to, as the browser resolves it; null when it cannot be parsed. */
const resolveHref = (href: string): URL | null => {
  try {
    return new URL(href, SITE);
  } catch {
    return null;
  }
};

/** A link to this site: it stays on the page's origin once the browser has resolved it ("/\\evil.com" does not). */
export const isSiteHref = (href: string): boolean => href.startsWith("/") && resolveHref(href)?.origin === SITE;

/** A link target the page may follow: an http(s) address, or a path that stays on this site. */
export const isSafeHref = (href: string): boolean => {
  const url = resolveHref(href);
  if (url === null || (url.protocol !== "http:" && url.protocol !== "https:")) return false;
  return /^https?:\/\//i.test(href) || isSiteHref(href);
};

export function parseMarkdown(source: string): Block[] {
  const blocks: Block[] = [];
  const lines = source.replace(/\r\n?/g, "\n").split("\n");
  let paragraph: string[] = [];
  const flush = (): void => {
    if (paragraph.length > 0) blocks.push({ kind: "paragraph", inline: parseInline(paragraph.join(" ")) });
    paragraph = [];
  };
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]!;
    if (line.trimStart().startsWith("```")) {
      flush();
      const body: string[] = [];
      i++;
      while (i < lines.length && !lines[i]!.trimStart().startsWith("```")) body.push(lines[i++]!);
      blocks.push({ kind: "fence", text: body.join("\n") });
      continue;
    }
    const heading = /^#{1,6}\s+(.*)$/.exec(line);
    if (heading) {
      flush();
      blocks.push({ kind: "heading", inline: parseInline(heading[1]!.trim()) });
      continue;
    }
    const item = /^\s*(?:([-*+])|(\d+)[.)])\s+(.*)$/.exec(line);
    if (item) {
      flush();
      const ordered = item[2] !== undefined;
      const last = blocks.at(-1);
      if (last?.kind === "list" && last.ordered === ordered) blocks[blocks.length - 1] = { ...last, items: [...last.items, parseInline(item[3]!)] };
      else blocks.push({ kind: "list", ordered, items: [parseInline(item[3]!)] });
      continue;
    }
    if (line.trim() === "") flush();
    else paragraph.push(line.trim());
  }
  flush();
  return blocks;
}

/** Inline markers, matched left to right at the earliest marker. */
function parseInline(text: string): Inline[] {
  const out: Inline[] = [];
  let rest = text;
  let plain = "";
  const pushPlain = (): void => {
    if (plain !== "") out.push({ kind: "text", text: plain });
    plain = "";
  };
  while (rest !== "") {
    let m: RegExpExecArray | null;
    if ((m = /^`([^`]+)`/.exec(rest))) {
      pushPlain();
      out.push({ kind: "code", text: m[1]! });
    } else if ((m = /^\*\*([^*]+)\*\*/.exec(rest)) || (m = /^__([^_]+)__/.exec(rest))) {
      pushPlain();
      out.push({ kind: "strong", inline: parseInline(m[1]!) });
    } else if ((m = /^\*([^*\s][^*]*)\*/.exec(rest)) || (m = /^_([^_\s][^_]*)_/.exec(rest))) {
      pushPlain();
      out.push({ kind: "emphasis", inline: parseInline(m[1]!) });
    } else if ((m = /^\[([^\]]+)\]\(((?:[^()\s]|\([^()\s]*\))+)\)/.exec(rest))) {
      const [, label, href] = m as unknown as [string, string, string];
      if (isSafeHref(href)) {
        pushPlain();
        const task = TASK_PAGE.exec(href);
        out.push(task ? { kind: "task", taskId: decodeURIComponent(task[1]!), href, title: label } : { kind: "link", href, inline: parseInline(label) });
      } else {
        plain += label;
      }
    } else {
      plain += rest[0]!;
      rest = rest.slice(1);
      continue;
    }
    rest = rest.slice(m[0].length);
  }
  pushPlain();
  return out;
}
