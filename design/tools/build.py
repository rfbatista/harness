#!/usr/bin/env python3
"""Build self-contained pages for the Design tab, whose sandbox loads nothing
beside the file.

design/<name>.html from design/src/<name>.html (skips _partials), filling:
  <style data-ds></style>     design-system/css/index.css, @imports inlined,
                              plus light and dark tokens scoped to [data-theme]
  <style data-kit></style>    design-system/session-screen.css + design/src/_kit.css
  <script data-kit></script>  design-system/session-screen.js
design/specimen.html from design-system/specimen.html: its stylesheet links and
the session-screen script inlined; the web fonts and specimen.js (which imports
../web) dropped, so the scripted dropdowns show as their native selects.
Usage: python3 design/tools/build.py  (from the worktree root)
"""
import pathlib, re

ROOT = pathlib.Path(__file__).resolve().parents[2]
CSS = ROOT / "design-system" / "css"
SRC = ROOT / "design" / "src"

def inline(path, seen):
    out = []
    for line in path.read_text().splitlines():
        m = re.match(r'\s*@import url\("(.+?)"\);', line)
        if m:
            target = (path.parent / m.group(1)).resolve()
            if target not in seen:
                seen.add(target)
                out.append(f"/* {target.relative_to(ROOT)} */")
                out.append(inline(target, seen))
            continue
        out.append(line)
    return "\n".join(out)

def block(text, selector):
    i = text.index(selector)
    j = text.index("{", i)
    return text[j + 1:text.index("}", j)]

tokens = (CSS / "global" / "tokens.css").read_text()
dark = block(tokens, ':root[data-theme="dark"] {')
roots = [m.start() for m in re.finditer(r"^  :root \{", tokens, re.M)]
light_src = next(block(tokens[i:], ":root {") for i in roots if "--color-canvas" in block(tokens[i:], ":root {"))
light = "\n".join(l for l in light_src.splitlines() if "--color-" in l or "--shadow-" in l)
scoped = (
    "\n/* Scoped themes for side-by-side frames (mockup only). */\n"
    "@layer global {\n"
    f'  [data-theme="light"] {{ color-scheme: light; {light}\n  }}\n'
    f'  [data-theme="dark"] {{ color-scheme: dark; {dark}\n  }}\n'
    "}\n"
)
ds = inline(CSS / "index.css", set()) + scoped
DS = ROOT / "design-system"
screen_css = (DS / "session-screen.css").read_text()
kit_css = screen_css + "\n" + (SRC / "_kit.css").read_text()
kit_js = (DS / "session-screen.js").read_text()

for src in sorted(SRC.glob("[!_]*.html")):
    page = src.read_text()
    page = page.replace("<style data-ds></style>", "<style data-ds>\n" + ds + "\n</style>", 1)
    page = page.replace("<style data-kit></style>", "<style data-kit>\n" + kit_css + "\n</style>", 1)
    page = page.replace("<script data-kit></script>", "<script data-kit>\n" + kit_js + "\n</script>", 1)
    (ROOT / "design" / src.name).write_text(page)
    print("built", "design/" + src.name, len(page), "bytes")

spec = (DS / "specimen.html").read_text()
spec = re.sub(r'\s*<link rel="preconnect"[^>]*/>', "", spec)
spec = re.sub(r'\s*<link\s+rel="stylesheet"\s+href="https://fonts\.googleapis[^>]*/>', "", spec, flags=re.S)
spec = spec.replace('<link rel="stylesheet" href="./css/index.css" />', "<style data-ds>\n" + ds + "\n</style>", 1)
spec = spec.replace('<link rel="stylesheet" href="./session-screen.css" />', "<style data-kit>\n" + screen_css + "\n</style>", 1)
spec = spec.replace('<script data-kit src="./session-screen.js"></script>', "<script data-kit>\n" + kit_js + "\n</script>", 1)
spec = spec.replace('<script type="module" src="./specimen.js"></script>', "", 1)
(ROOT / "design" / "specimen.html").write_text(spec)
print("built design/specimen.html", len(spec), "bytes")
