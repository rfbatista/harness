"""WCAG contrast of OKLCH colours over design-system surfaces, with alpha compositing.
Usage: python3 -I contrast.py  (edit CANDIDATES below)."""
import math, re, sys, json

def oklch_to_lin(L, C, h):
    a, b = C*math.cos(math.radians(h)), C*math.sin(math.radians(h))
    l_ = L + 0.3963377774*a + 0.2158037573*b
    m_ = L - 0.1055613458*a - 0.0638541728*b
    s_ = L - 0.0894841775*a - 1.2914855480*b
    l, m, s = l_**3, m_**3, s_**3
    r = 4.0767416621*l - 3.3077115913*m + 0.2309699292*s
    g = -1.2684380046*l + 2.6097574011*m - 0.3413193965*s
    bb = -0.0041960863*l - 0.7034186147*m + 1.7076147010*s
    return [min(1, max(0, x)) for x in (r, g, bb)]

def parse(s):
    m = re.match(r"oklch\(([\d.]+) ([\d.]+) ([\d.]+)(?: / ([\d.]+))?\)", s)
    L, C, h, a = m.groups()
    return oklch_to_lin(float(L), float(C), float(h)), float(a) if a else 1.0

def enc(x): return 12.92*x if x <= 0.0031308 else 1.055*x**(1/2.4) - 0.055
def dec(x): return x/12.92 if x <= 0.04045 else ((x+0.055)/1.055)**2.4
def over(fg, bg):
    (c, a) = fg  # browsers composite in gamma-encoded sRGB
    return [dec(a*enc(x) + (1-a)*enc(y)) for x, y in zip(c, bg)]

def lum(c): return 0.2126*c[0] + 0.7152*c[1] + 0.0722*c[2]
def ratio(a, b):
    la, lb = sorted((lum(a), lum(b)), reverse=True)
    return (la + 0.05) / (lb + 0.05)

if __name__ == "__main__":
    tokens = json.load(open(sys.argv[1]))
    worst = []
    for theme, t in tokens["themes"].items():
        col = t["color"]
        base = parse(col.get("code", col["sunken"]))[0]
        surfaces = {"code": base}
        for w in ("diff-add-wash", "diff-del-wash", "target-wash"):
            if w in col: surfaces[w] = over(parse(col[w]), base)
        for name, v in col.items():
            if not (name.startswith("tok-") or name in ("ink", "ink-muted", "ink-faint", "diff-add", "diff-del", "line-number")): continue
            fg = over(parse(v), base)
            rs = {s: ratio(fg, b) for s, b in surfaces.items()}
            low = min(rs.values())
            print(f"{theme:5} {name:16} " + " ".join(f"{s}={r:4.2f}" for s, r in rs.items()) + ("  <-- FAIL" if low < 4.5 else ""))
        canvas = parse(col["canvas"])[0]; panel = parse(col["panel"])[0]
        for name, v in col.items():
            if name.startswith("context-"):
                fg = parse(v)[0]
                r1, r2 = ratio(fg, canvas), ratio(fg, panel)
                print(f"{theme:5} {name:16} canvas={r1:4.2f} panel={r2:4.2f}" + ("  <-- <3" if min(r1, r2) < 3 else ""))
