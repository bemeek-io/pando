// Topographic terrain behind the console.
//
// Generated, not drawn: a height field made of irregular hills, with contour
// lines traced through it by marching squares and smoothed into curves. Lines
// at different heights can never cross, because they are level sets of one
// surface — which is what makes it read as a real survey map rather than a
// pattern.
//
// It is part of the page, not pinned to the window: it scrolls with the
// content. To cover a page of any length it is a tile, and the terrain is
// periodic — hills wrap around the tile's edges and the warp uses whole
// periods — so the lines meet across every seam and there is no edge to see.
//
// One colour, one line width, one opacity across the whole thing, and no index
// contours. Two strengths: `quiet`, behind working screens, where it must never
// compete with a table; and `full`, on the sign-in screens, which have nothing
// else on them and can carry the whole map. Each page gets its own terrain from
// a seed (the section, and the app when there is one); the same seed always
// gives the same map.

import { useMemo } from 'react';

// One tile, in CSS pixels. Large, so a repeat is rarely in view at once.
const COLS = 160;
const ROWS = 120;
const CELL = 10;
const TAU = Math.PI * 2;

const STRENGTH = {
  quiet: { levels: 12, opacity: 0.35 },
  full: { levels: 20, opacity: 0.8 },
} as const;
type Strength = keyof typeof STRENGTH;

type Pt = [number, number];

/** FNV-1a, so a route string becomes a stable 32-bit seed. */
function hash(s: string): number {
  let h = 0x811c9dc5;
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }
  return h >>> 0;
}

/** mulberry32: small, fast, and the same sequence for the same seed. */
function rng(seed: number): () => number {
  let a = seed;
  return () => {
    a = (a + 0x6d2b79f5) | 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

/** Distance on a unit circle, so a hill near one edge continues past it. */
function wrap(d: number): number {
  const a = Math.abs(d) % 1;
  return Math.min(a, 1 - a);
}

function generate(seed: string, levels: number): string {
  const r = rng(hash(seed));
  const between = (lo: number, hi: number) => lo + r() * (hi - lo);

  const hills = Array.from({ length: 7 + Math.floor(r() * 4) }, () => ({
    cx: r(),
    cy: r(),
    sx: between(0.07, 0.18),
    sy: between(0.08, 0.2),
    a: between(0.3, 1),
  }));
  const p = Array.from({ length: 6 }, () => between(0, TAU));

  // Every term is periodic over the tile, so opposite edges have equal
  // heights and the contours line up across the seam.
  const height = (x: number, y: number) => {
    const wx = x + 0.03 * Math.sin(TAU * 2 * y + p[0]!) + 0.015 * Math.sin(TAU * 5 * y + p[1]!);
    const wy = y + 0.03 * Math.sin(TAU * 2 * x + p[2]!) + 0.015 * Math.sin(TAU * 4 * x + p[3]!);
    let h = 0;
    for (const k of hills) {
      const dx = wrap(wx - k.cx) / k.sx;
      const dy = wrap(wy - k.cy) / k.sy;
      h += k.a * Math.exp(-(dx * dx + dy * dy));
    }
    return h + 0.05 * Math.sin(TAU * x + p[4]!) * Math.cos(TAU * y + p[5]!);
  };

  return trace(height, levels);
}

/** Every contour of the tile, as one path. */
function trace(height: (x: number, y: number) => number, levels: number): string {
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

  let out = '';
  const lo = min + (max - min) * 0.06;
  const hi = max - (max - min) * 0.04;

  for (let l = 0; l < levels; l++) {
    const t = lo + ((hi - lo) * l) / (levels - 1);
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

    // Join segments into lines: open ones first (they end at the tile edge,
    // where the neighbouring tile continues them), then closed loops.
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
      return chain.map((k) => pts.get(k) ?? [0, 0]);
    };

    const ends = [...adj.keys()].filter((k) => adj.get(k)?.length === 1);
    for (const k of [...ends, ...adj.keys()]) {
      if (used.has(k)) continue;
      const line = walk(k);
      if (line.length >= 2) out += smooth(line);
    }
  }
  return out;
}

/** A curve through the midpoints of a polyline, so traced steps read as terrain.
 *  Endpoints are kept exact, so lines still meet their neighbours at a seam. */
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

const cache = new Map<string, string>();

/**
 * Place as the first child of a page root that has `position: relative` and
 * `isolation: isolate`. It fills that root — its full scrolling height — and
 * sits above the root's paper and below everything else.
 */
export function TopoBackground({
  seed,
  strength = 'quiet',
}: {
  seed: string;
  strength?: Strength;
}) {
  const { levels, opacity } = STRENGTH[strength];
  const key = `${seed}|${levels}`;

  const d = useMemo(() => {
    let t = cache.get(key);
    if (t === undefined) {
      t = generate(seed, levels);
      cache.set(key, t);
    }
    return t;
  }, [key, seed, levels]);

  const id = `topo-${hash(key).toString(36)}`;

  return (
    <svg
      aria-hidden="true"
      style={{
        position: 'absolute',
        inset: 0,
        width: '100%',
        height: '100%',
        zIndex: -1,
        pointerEvents: 'none',
        opacity,
      }}
    >
      <defs>
        <pattern id={id} width={COLS * CELL} height={ROWS * CELL} patternUnits="userSpaceOnUse">
          <path
            d={d}
            fill="none"
            stroke="var(--contour-line)"
            strokeWidth="var(--contour-line-width)"
            strokeLinecap="round"
            strokeLinejoin="round"
          />
        </pattern>
      </defs>
      <rect width="100%" height="100%" fill={`url(#${id})`} />
    </svg>
  );
}
