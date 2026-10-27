//go:build !windows

package web_test

import (
	"encoding/json"
	"strings"

	"github.com/chromedp/chromedp"
)

// a11yAuditJS is an in-page audit of the screen as it stands. It returns the
// list of what a keyboard or screen-reader user would trip on, each as
// "kind: element: detail" (empty when the screen is clean):
//
//   - name: a visible control without an accessible name;
//   - invalid: an aria-invalid field not linked to its message;
//   - idref: aria-controls, labelledby, describedby or activedescendant naming
//     an id that is not in the page, and duplicate ids;
//   - group: a visible native checkbox that is one of a list and in no fieldset or role=group;
//   - target: a visible control under 24 by 24 CSS pixels with another control within 12 px of its centre (WCAG 2.5.8);
//   - contrast: visible text below 4.5:1 (3:1 for large text) against the
//     colour behind it, computed from the styles as rendered, so the scheme
//     the page is in is the one measured.
//
// Elements named by a `skip` selector in a data-a11y-skip ancestor are left out.
const a11yAuditJS = `(() => {
  const out = [];
  const vis = (el) => el.checkVisibility({ checkVisibilityCSS: true });
  const skipped = (el) => !!el.closest('[data-a11y-skip]');
  const describe = (el) => {
    const t = el.tagName.toLowerCase();
    const data = [...el.attributes].find((a) => a.name.startsWith('data-') && a.name !== 'data-a11y-skip');
    const txt = (el.textContent || '').replace(/\s+/g, ' ').trim().slice(0, 30);
    return t + (el.id ? '#' + el.id : '') + (data ? '[' + data.name + (data.value ? '=' + data.value : '') + ']' : '') + (txt ? ' "' + txt + '"' : '');
  };
  const nameOf = (el) => {
    const lb = el.getAttribute('aria-labelledby');
    if (lb) return lb.split(/\s+/).map((i) => document.getElementById(i)?.textContent ?? '').join(' ').trim();
    const al = el.getAttribute('aria-label');
    if (al && al.trim()) return al.trim();
    if (el.labels && el.labels.length) return [...el.labels].map((l) => l.textContent).join(' ').trim();
    if (el.tagName === 'INPUT' && ['button', 'submit'].includes(el.type)) return el.value;
    const t = (el.textContent || '').trim();
    if (t) return t;
    return (el.getAttribute('title') || '').trim();
  };
  const interactive = [...document.querySelectorAll('a[href], button, input:not([type=hidden]), select, textarea, [role=tab], [role=menuitem], [role=option], [role=combobox], [role=switch]')]
    .filter((el) => vis(el) && !skipped(el) && !el.closest('[inert]'));

  for (const el of interactive) {
    if (!nameOf(el)) out.push('name: ' + describe(el) + ': no accessible name');
  }
  for (const el of document.querySelectorAll('[aria-invalid="true"]')) {
    if (!vis(el) || skipped(el)) continue;
    const ids = (el.getAttribute('aria-describedby') || '').split(/\s+/).filter(Boolean);
    const msgs = ids.map((i) => document.getElementById(i)).filter((m) => m && (m.textContent || '').trim());
    if (!msgs.length) out.push('invalid: ' + describe(el) + ': aria-invalid without a described message');
  }
  const seen = new Map();
  for (const el of document.querySelectorAll('[id]')) seen.set(el.id, (seen.get(el.id) || 0) + 1);
  for (const [id, n] of seen) if (n > 1) out.push('idref: #' + id + ': ' + n + ' elements share the id');
  for (const el of document.querySelectorAll('[aria-controls],[aria-labelledby],[aria-describedby],[aria-activedescendant]')) {
    if (!vis(el) || skipped(el)) continue;
    for (const attr of ['aria-controls', 'aria-labelledby', 'aria-describedby', 'aria-activedescendant']) {
      for (const id of (el.getAttribute(attr) || '').split(/\s+/).filter(Boolean)) {
        if (!document.getElementById(id)) out.push('idref: ' + describe(el) + ': ' + attr + ' names missing #' + id);
      }
    }
  }
  for (const el of interactive) {
    const list = el.parentElement?.parentElement;
    if (el.type === 'checkbox' && !el.closest('fieldset, [role=group]') && list && list.querySelectorAll('input[type=checkbox]').length > 1) out.push('group: ' + describe(el) + ': checkbox in no fieldset or role=group');
  }
  const boxOf = (el) => {
    let box = el;
    if (el.type === 'checkbox' || el.type === 'radio') box = el.labels?.[0] ?? el.closest('label') ?? el;
    return box.getBoundingClientRect();
  };
  const small = interactive.filter((el) => {
    const r = boxOf(el);
    return r.width > 1 && !(el.tagName === 'A' && getComputedStyle(el).display === 'inline') && (r.width < 23.5 || r.height < 23.5);
  });
  for (const el of small) {
    // WCAG 2.5.8: a small target passes when a 24 px circle on its centre
    // meets no other target.
    const r = boxOf(el);
    const cx0 = r.left + r.width / 2;
    const cy0 = r.top + r.height / 2;
    let near = '';
    const crowded = interactive.some((o) => {
      if (o === el || o.contains(el) || el.contains(o)) return false;
      const q = boxOf(o);
      const dx = Math.max(q.left - cx0, 0, cx0 - q.right);
      const dy = Math.max(q.top - cy0, 0, cy0 - q.bottom);
      if (Math.hypot(dx, dy) >= 12) return false;
      near = describe(o);
      return true;
    });
    if (crowded) out.push('target: ' + describe(el) + ': ' + Math.round(r.width) + 'x' + Math.round(r.height) + ' near ' + near);
  }

  // Contrast.
  const cv = document.createElement('canvas');
  cv.width = cv.height = 1;
  const cx = cv.getContext('2d', { willReadFrequently: true });
  const rgba = (css) => {
    cx.clearRect(0, 0, 1, 1);
    cx.fillStyle = '#000';
    cx.fillStyle = css;
    cx.fillRect(0, 0, 1, 1);
    const d = cx.getImageData(0, 0, 1, 1).data;
    return [d[0], d[1], d[2], d[3] / 255];
  };
  const over = (top, bottom) => {
    const a = top[3] + bottom[3] * (1 - top[3]);
    if (a === 0) return [0, 0, 0, 0];
    return [0, 1, 2].map((i) => (top[i] * top[3] + bottom[i] * bottom[3] * (1 - top[3])) / a).concat([a]);
  };
  const lum = ([r, g, b]) => {
    const f = (v) => { v /= 255; return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4; };
    return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(b);
  };
  const backdrop = (el) => {
    const layers = [];
    for (let n = el; n; n = n.parentElement) {
      const cs = getComputedStyle(n);
      if (cs.backgroundImage !== 'none') return null;
      const c = rgba(cs.backgroundColor);
      if (c[3] > 0) layers.push(c);
      if (c[3] === 1) break;
    }
    let acc = rgba(getComputedStyle(document.documentElement).backgroundColor);
    if (acc[3] === 0) acc = [255, 255, 255, 1];
    for (const c of layers.reverse()) acc = over(c, acc);
    return acc;
  };
  const opacityOf = (el) => { let o = 1; for (let n = el; n; n = n.parentElement) o *= parseFloat(getComputedStyle(n).opacity); return o; };
  const checked = new Set();
  const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
  for (let tn = walker.nextNode(); tn; tn = walker.nextNode()) {
    if (!tn.nodeValue.trim()) continue;
    const el = tn.parentElement;
    if (!el || checked.has(el) || el.closest('script,style,template,svg,[hidden],[data-a11y-skip]') || !vis(el)) continue;
    checked.add(el);
    if (el.closest(':disabled, [aria-disabled=true]')) continue;
    const cs = getComputedStyle(el);
    const bg = backdrop(el);
    if (!bg) continue;
    let fg = rgba(cs.color);
    fg[3] *= opacityOf(el);
    fg = over(fg, bg);
    const [l1, l2] = [lum(fg), lum(bg)].sort((a, b) => b - a);
    const ratio = (l1 + 0.05) / (l2 + 0.05);
    const px = parseFloat(cs.fontSize);
    const large = px >= 24 || (px >= 18.66 && parseInt(cs.fontWeight, 10) >= 700);
    if (ratio < (large ? 3 : 4.5)) out.push('contrast: ' + describe(el) + ': ' + ratio.toFixed(2) + ':1 (' + cs.color + ' on rgb(' + bg.slice(0, 3).map(Math.round) + ')) needs ' + (large ? 3 : 4.5));
  }
  return [...new Set(out)];
})()`

// a11yAudit runs the audit and puts the findings in into.
func a11yAudit(into *[]string) chromedp.Action {
	return chromedp.Evaluate(a11yAuditJS, into)
}

// a11yJoin is findings as one message.
func a11yJoin(findings []string) string {
	raw, _ := json.MarshalIndent(findings, "", " ")
	return strings.TrimSpace(string(raw))
}
