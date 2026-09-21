// Topographic terrain behind the console.
//
// Generated, not drawn: a height field made of a few irregular hills, with
// contour lines traced through it by marching squares and smoothed into
// curves. Lines at different heights can never cross, because they are level
// sets of one surface — which is what makes it read as a real survey map
// rather than a pattern.
//
// It sits in the bottom-right corner, fixed to the viewport, and fades out
// toward the content with a mask, so it is behind the console rather than
// wallpaper across it. Every fifth line is an index contour, as on a printed
// quadrangle. Colors are the contour tokens, so the night-survey theme follows.
//
// Deterministic: the same terrain on every load, computed once per session.

const COLS = 120;
const ROWS = 80;
const CELL = 10;
const LEVELS = 18;

type Pt = [number, number];

interface Hill {
  cx: number;
  cy: number;
  sx: number;
  sy: number;
  a: number;
}

// Weighted toward the bottom-right, which is where the layer is anchored.
const HILLS: Hill[] = [
  { cx: 0.8, cy: 0.74, sx: 0.2, sy: 0.17, a: 1.0 },
  { cx: 0.56, cy: 0.93, sx: 0.15, sy: 0.12, a: 0.62 },
  { cx: 0.97, cy: 0.36, sx: 0.17, sy: 0.2, a: 0.72 },
  { cx: 0.42, cy: 0.6, sx: 0.19, sy: 0.22, a: 0.34 },
  { cx: 0.68, cy: 0.45, sx: 0.1, sy: 0.09, a: 0.28 },
];

function height(x: number, y: number): number {
  // A gentle warp so hills are irregular rather than elliptical.
  const wx = x + 0.035 * Math.sin(11 * y + 2.1) + 0.02 * Math.sin(23 * y + 0.4);
  const wy = y + 0.035 * Math.sin(13 * x + 0.5) + 0.02 * Math.sin(19 * x + 1.7);
  let h = 0;
  for (const k of HILLS) {
    const dx = (wx - k.cx) / k.sx;
    const dy = (wy - k.cy) / k.sy;
    h += k.a * Math.exp(-(dx * dx + dy * dy));
  }
  return h + 0.05 * Math.sin(7 * x + 1.3) * Math.cos(6 * y + 0.2);
}

/** Contour paths, one entry per level, bottom level first. */
function trace(): { d: string; index: boolean }[] {
  const w = COLS + 1;
  const v = new Float64Array(w * (ROWS + 1));
  let min = Infinity;
  let max = -Infinity;
  for (let j = 0; j <= ROWS; j++) {
    for (let i = 0; i <= COLS; i++) {
      const h = height(i / COLS, j / ROWS);
      v[j * w + i] = h;
      if (h < min) min = h;
      if (h > max) max = h;
    }
  }
  const at = (i: number, j: number): number => v[j * w + i] ?? 0;

  const out: { d: string; index: boolean }[] = [];
  const lo = min + (max - min) * 0.1;
  const hi = max - (max - min) * 0.03;

  for (let l = 0; l < LEVELS; l++) {
    const t = lo + ((hi - lo) * l) / (LEVELS - 1);
    const pts = new Map<string, Pt>();
    const adj = new Map<string, string[]>();

    const edge = (key: string, x1: number, y1: number, v1: number, x2: number, y2: number, v2: number) => {
      if (!pts.has(key)) {
        const f = (t - v1) / (v2 - v1);
        pts.set(key, [(x1 + (x2 - x1) * f) * CELL, (y1 + (y2 - y1) * f) * CELL]);
      }
      return key;
    };
    const link = (a: string, b: string) => {
      (adj.get(a) ?? adj.set(a, []).get(a)!).push(b);
      (adj.get(b) ?? adj.set(b, []).get(b)!).push(a);
    };

    for (let j = 0; j < ROWS; j++) {
      for (let i = 0; i < COLS; i++) {
        const a = at(i, j);
        const b = at(i + 1, j);
        const c = at(i + 1, j + 1);
        const d = at(i, j + 1);
        const idx = (a >= t ? 8 : 0) | (b >= t ? 4 : 0) | (c >= t ? 2 : 0) | (d >= t ? 1 : 0);
        if (idx === 0 || idx === 15) continue;

        const top = () => edge(`h${i},${j}`, i, j, a, i + 1, j, b);
        const right = () => edge(`v${i + 1},${j}`, i + 1, j, b, i + 1, j + 1, c);
        const bottom = () => edge(`h${i},${j + 1}`, i, j + 1, d, i + 1, j + 1, c);
        const left = () => edge(`v${i},${j}`, i, j, a, i, j + 1, d);
        const centre = (a + b + c + d) / 4 >= t;

        switch (idx) {
          case 1: case 14: link(left(), bottom()); break;
          case 2: case 13: link(bottom(), right()); break;
          case 3: case 12: link(left(), right()); break;
          case 4: case 11: link(top(), right()); break;
          case 6: case 9: link(top(), bottom()); break;
          case 7: case 8: link(left(), top()); break;
          case 5:
            if (centre) { link(left(), top()); link(bottom(), right()); }
            else { link(top(), right()); link(left(), bottom()); }
            break;
          case 10:
            if (centre) { link(top(), right()); link(left(), bottom()); }
            else { link(left(), top()); link(bottom(), right()); }
            break;
        }
      }
    }

    // Join segments into lines: open ones first (they end at the border),
    // then the closed loops that remain.
    const used = new Set<string>();
    const walk = (start: string): Pt[] => {
      const chain = [start];
      used.add(start);
      let cur = start;
      for (;;) {
        const next = (adj.get(cur) ?? []).find((k) => !used.has(k));
        if (!next) break;
        chain.push(next);
        used.add(next);
        cur = next;
      }
      if (chain.length > 2 && (adj.get(cur) ?? []).includes(start)) chain.push(start);
      return chain.map((k) => pts.get(k)!);
    };

    let d = '';
    const ends = [...adj.keys()].filter((k) => adj.get(k)!.length === 1);
    for (const k of [...ends, ...adj.keys()]) {
      if (used.has(k)) continue;
      const line = walk(k);
      if (line.length >= 3) d += smooth(line);
    }
    out.push({ d, index: l % 5 === 4 });
  }
  return out;
}

/** A curve through the midpoints of a polyline, so traced steps read as terrain. */
function smooth(p: Pt[]): string {
  const f = (n: number) => n.toFixed(1);
  const pt = (i: number): Pt => p[i] ?? [0, 0];
  let d = `M${f(pt(0)[0])} ${f(pt(0)[1])}`;
  for (let i = 1; i < p.length - 1; i++) {
    const [x, y] = pt(i);
    const [nx, ny] = pt(i + 1);
    d += `Q${f(x)} ${f(y)} ${f((x + nx) / 2)} ${f((y + ny) / 2)}`;
  }
  const [lx, ly] = pt(p.length - 1);
  return d + `L${f(lx)} ${f(ly)}`;
}

let cached: { d: string; index: boolean }[] | null = null;

export function TopoBackground() {
  cached ??= trace();

  // Fades from the bottom-right corner toward the content.
  const mask = 'radial-gradient(ellipse 100% 100% at 100% 100%, black 30%, transparent 78%)';

  return (
    <svg
      aria-hidden="true"
      viewBox={`0 0 ${COLS * CELL} ${ROWS * CELL}`}
      preserveAspectRatio="xMaxYMax slice"
      style={{
        position: 'fixed',
        right: 0,
        bottom: 0,
        width: '72vw',
        height: '88vh',
        zIndex: -1,
        pointerEvents: 'none',
        opacity: 0.8,
        maskImage: mask,
        WebkitMaskImage: mask,
      }}
    >
      {cached.map(({ d, index }, i) => (
        <path
          key={i}
          d={d}
          fill="none"
          stroke={index ? 'var(--contour)' : 'var(--contour-line)'}
          strokeWidth={index ? 'var(--contour-index-width)' : 'var(--contour-line-width)'}
          vectorEffect="non-scaling-stroke"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      ))}
    </svg>
  );
}
