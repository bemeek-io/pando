import React from 'react';
import { MapCollar } from './MapCollar.jsx';

/* Deterministic terrain: one shape function scaled per ring, so rings are
   irregular, roughly concentric, and can never cross. */
function shape(t) {
  return 1 + 0.10 * Math.sin(3 * t + 0.7) + 0.07 * Math.sin(5 * t + 2.1) + 0.045 * Math.sin(7 * t + 4.4) + 0.03 * Math.sin(2 * t + 1.3);
}

function ringPath(cx, cy, r, kx, ky, samples) {
  const pts = [];
  for (let i = 0; i < samples; i++) {
    const t = (i / samples) * Math.PI * 2;
    const rr = r * shape(t);
    pts.push([cx + Math.cos(t) * rr * kx, cy + Math.sin(t) * rr * ky]);
  }
  const mid = (a, b) => [(a[0] + b[0]) / 2, (a[1] + b[1]) / 2];
  let d = 'M ' + mid(pts[pts.length - 1], pts[0]).map(n => n.toFixed(2)).join(' ');
  for (let i = 0; i < pts.length; i++) {
    const p = pts[i];
    const m = mid(p, pts[(i + 1) % pts.length]);
    d += ` Q ${p[0].toFixed(2)} ${p[1].toFixed(2)} ${m[0].toFixed(2)} ${m[1].toFixed(2)}`;
  }
  return d + ' Z';
}

export function ContourMap({
  size = 520,
  rings,
  collar = false,
  summit = true,
  animate = false,
  coordinates = ['38°31′30″N', '111°45′00″W'],
  elevation = '9,140 ft',
  scaleLabels = ['0', '½', '1 mi'],
  style,
  ...rest
}) {
  const hero = size >= 320;
  const count = rings || (hero ? 8 : 4);
  const w = size;
  const h = Math.round(size * 0.72);
  const pad = collar ? 26 : 4;
  const cx = w * 0.52;
  const cy = h * 0.48;
  const spacing = hero ? (h - pad * 2) / (count * 2.6) : Math.max(6, (h - pad * 2) / (count * 2.6));
  const inner = spacing * 0.9;
  const paths = [];
  for (let i = count - 1; i >= 0; i--) {
    const r = inner + i * spacing * (1 + 0.06 * Math.sin(i * 1.7));
    const index = i % 5 === 0;
    paths.push({ d: ringPath(cx, cy, r, 1.22, 0.86, hero ? 72 : 48), index, i });
  }
  const summitSize = hero ? 8 : 6;
  const dur = 900;

  // The collar is MapCollar's job, so the corner ticks and the scale bar have
  // one definition rather than a copy here and a copy in every page that wants
  // to read as a printed sheet. Uncollared, the figure is the bare terrain and
  // there is no frame at all.
  const Frame = collar ? MapCollar : PlainFrame;
  const frameProps = collar
    ? { marginalia: coordinates, scale: true, scaleLabels }
    : {};

  return (
    <figure style={{ margin: 0, width: w, maxWidth: '100%', ...style }} {...rest}>
      <Frame {...frameProps}>
        <svg width="100%" viewBox={`0 0 ${w} ${h}`} style={{ display: 'block', overflow: 'visible' }} aria-hidden="true">
          {paths.map(({ d, index, i }) => (
            <path
              key={i} d={d} fill="none"
              stroke={index ? 'var(--contour)' : 'var(--contour-line)'}
              strokeWidth={index ? 'var(--contour-index-width)' : 'var(--contour-line-width)'}
              pathLength={animate ? 1 : undefined}
              style={animate ? { '--dash': 1, strokeDasharray: 1, animation: `pando-draw ${dur}ms var(--ease) ${(count - 1 - i) * (dur / count / 2)}ms both` } : undefined}
            />
          ))}
          {summit && (hero ? (
            <polygon
              points={`${cx},${cy - summitSize * 0.62} ${cx + summitSize * 0.62},${cy + summitSize * 0.5} ${cx - summitSize * 0.62},${cy + summitSize * 0.5}`}
              fill="var(--marker)"
              style={animate ? { animation: `pando-fade var(--dur-base) var(--ease) ${dur}ms both` } : undefined}
            />
          ) : (
            <circle cx={cx} cy={cy} r={summitSize / 2} fill="var(--marker)" />
          ))}
          {collar && elevation && (
            <g>
              <rect x={cx + spacing * 2.2} y={cy - 7} width={elevation.length * 7.4} height={14} fill="var(--paper)" />
              <text x={cx + spacing * 2.4} y={cy + 4} style={{ font: 'var(--type-code-sm)', fill: 'var(--contour-text)' }}>{elevation}</text>
            </g>
          )}
        </svg>
      </Frame>
    </figure>
  );
}

/** No collar: the terrain prints straight onto the page. */
function PlainFrame({ children }) {
  return <React.Fragment>{children}</React.Fragment>;
}
