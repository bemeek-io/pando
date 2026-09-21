// Topographic terrain behind the console.
//
// Generated, not drawn: a height field made of irregular hills, with contour
// lines traced through it by marching squares and smoothed into curves. Lines
// at different heights can never cross, because they are level sets of one
// surface — which is what makes it read as a real survey map rather than a
// pattern.
//
// Each page gets its own terrain. The hills, the warp and where the map shows
// through are all drawn from a seed — the section, and the app when there is
// one — so moving through the console moves across different ground, while the
// tabs of one app share that app's map. The same seed always gives the same
// map.
//
// It spans the viewport but shows through a few soft patches placed by the
// same seed, so it is never uniform wallpaper and never parked in one corner.
// It is kept faint: contour tokens at low opacity, and index contours at the
// ordinary line width, distinguished by colour alone.

import { useMemo } from 'react';

const COLS = 120;
const ROWS = 80;
const CELL = 10;
const LEVELS = 16;

type Pt = [number, number];
type Contour = { d: string; index: boolean };
interface Terrain {
  contours: Contour[];
  mask: string;
}

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

function generate(seed: string): Terrain {
  const r = rng(hash(seed));
  const between = (lo: number, hi: number) => lo + r() * (hi - lo);

  const hills = Array.from({ length: 6 + Math.floor(r() * 3) }, () => ({
    cx: between(-0.05, 1.05),
    cy: between(-0.05, 1.05),
    sx: between(0.08, 0.22),
    sy: between(0.08, 0.2),
    a: between(0.3, 1),
  }));
  const p = Array.from({ length: 6 }, () => between(0, Math.PI * 2));

  const height = (x: number, y: number) => {
    // A warp so hills are irregular rather than elliptical.
    const wx = x + 0.035 * Math.sin(11 * y + p[0]!) + 0.02 * Math.sin(23 * y + p[1]!);
    const wy = y + 0.035 * Math.sin(13 * x + p[2]!) + 0.02 * Math.sin(19 * x + p[3]!);
    let h = 0;
    for (const k of hills) {
      const dx = (wx - k.cx) / k.sx;
      const dy = (wy - k.cy) / k.sy;
      h += k.a * Math.exp(-(dx * dx + dy * dy));
    }
    return h + 0.05 * Math.sin(7 * x + p[4]!) * Math.cos(6 * y + p[5]!);
  };

  // Where the map shows through: a few soft patches. None is centred on the
  // top-left, which is where every page's heading and first rows are.
  const patches = Array.from({ length: 3 }, () => {
    let x = between(0.1, 1.05);
    let y = between(0.1, 1.05);
    if (x < 0.4 && y < 0.35) y += 0.45;
    const s = between(30, 50);
    return `radial-gradient(ellipse ${s.toFixed(0)}% ${(s * 1.25).toFixed(0)}% at ${(x * 100).toFixed(0)}% ${(y * 100).toFixed(0)}%, black 15%, transparent 100%)`;
  });

  return { contours: trace(height), mask: patches.join(', ') };
}

/** Contour paths, one entry per level, bottom level first. */
function trace(height: (x: number, y: number) => number): Contour[] {
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

  const out: Contour[] = [];
  const lo = min + (max - min) * 0.08;
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
      return chain.map((k) => pts.get(k) ?? [0, 0]);
    };

    let d = '';
    const ends = [...adj.keys()].filter((k) => adj.get(k)?.length === 1);
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

const cache = new Map<string, Terrain>();

export function TopoBackground({ seed }: { seed: string }) {
  const terrain = useMemo(() => {
    let t = cache.get(seed);
    if (!t) {
      t = generate(seed);
      cache.set(seed, t);
    }
    return t;
  }, [seed]);

  return (
    <svg
      aria-hidden="true"
      viewBox={`0 0 ${COLS * CELL} ${ROWS * CELL}`}
      preserveAspectRatio="xMidYMid slice"
      style={{
        position: 'fixed',
        inset: 0,
        width: '100vw',
        height: '100vh',
        zIndex: -1,
        pointerEvents: 'none',
        opacity: 0.5,
        maskImage: terrain.mask,
        WebkitMaskImage: terrain.mask,
      }}
    >
      {terrain.contours.map(({ d, index }, i) => (
        <path
          key={i}
          d={d}
          fill="none"
          stroke={index ? 'var(--contour)' : 'var(--contour-line)'}
          strokeWidth="var(--contour-line-width)"
          vectorEffect="non-scaling-stroke"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      ))}
    </svg>
  );
}
