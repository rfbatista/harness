"""Builds the self-contained code-views mockups.

Each page in ../src/ carries <style data-ds></style>, <style data-proposal></style>
and <script data-kit></script>. This fills the first with
design-system/css/index.css (imports inlined), the second with both themes
scoped to [data-theme] elements, the script with src/kit.js (fixtures and
renderers), and writes the page to ../<name>.html. The Design tab sandbox
loads no sibling folders, hence the inlining.

Run from anywhere: python3 -I design/code-views/tools/build.py
"""
import json, pathlib, re

HERE = pathlib.Path(__file__).resolve().parent
ROOT = HERE.parents[2]
DS = ROOT / "design-system"


def inline(path: pathlib.Path) -> str:
    css = path.read_text()
    return re.sub(r'@import url\("\./([^"]+)"\);',
                  lambda m: inline(path.parent / m.group(1)), css)


def scoped_themes() -> str:
    """Both themes' colours on [data-theme] elements, so a page can draw light beside dark."""
    tokens = json.loads((DS / "tokens.json").read_text())
    out = ["@layer global {"]
    for theme in ("light", "dark"):
        out.append(f'  [data-theme="{theme}"] {{')
        out.append(f"    color-scheme: {theme};")
        out += [f"    --color-{k}: {v};" for k, v in tokens["themes"][theme]["color"].items()]
        out.append(f"    --shadow-float: {tokens['themes'][theme]['shadow']['float']};")
        out.append("    color: var(--color-ink);")
        out.append("  }")
    out.append("}")
    return "\n".join(out)


def main() -> None:
    ds, prop = inline(DS / "css" / "index.css"), scoped_themes()
    kit = (HERE.parent / "src" / "kit.js").read_text()
    for src in sorted((HERE.parent / "src").glob("*.html")):
        page = src.read_text()
        page = page.replace("<style data-ds></style>", f"<style data-ds>\n{ds}\n</style>")
        page = page.replace("<style data-proposal></style>", f"<style data-proposal>\n{prop}\n</style>")
        page = page.replace("<script data-kit></script>", f"<script data-kit>\n{kit}\n</script>")
        (HERE.parent / src.name).write_text(page)
        print("built", src.name)
    specimen(ds)


def specimen(ds: str) -> None:
    """A self-contained copy of design-system/specimen.html for the Design tab."""
    page = (DS / "specimen.html").read_text()
    page = re.sub(r'<link rel="stylesheet" href="\./css/index\.css" />', lambda m: f"<style>\n{ds}\n</style>", page)
    page = re.sub(r'\s*<link rel="(preconnect|stylesheet)" href="https://fonts[^>]*/>', "", page)
    page = re.sub(r'\s*<link\s+rel="stylesheet"\s+href="https://fonts[^>]*/>', "", page, flags=re.S)
    page = page.replace('<script type="module" src="./specimen.js"></script>', "")
    (HERE.parent / "specimen.html").write_text(page)
    print("built specimen.html")


if __name__ == "__main__":
    main()
