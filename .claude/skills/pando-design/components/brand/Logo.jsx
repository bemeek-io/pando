import React from 'react';

/** Nested contour rings with a red summit dot, plus the lowercase wordmark. */
export function Logo({ size = 24, wordmark = true, style, ...rest }) {
  const small = size < 24;
  const rings = small ? 2 : 3;
  const sw = small ? 1.25 : 1.5;
  const box = size;
  const c = box / 2;
  return (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: size * 0.34, ...style }} {...rest}>
      <svg width={box} height={box} viewBox={`0 0 ${box} ${box}`} aria-hidden="true" style={{ display: 'block', flex: '0 0 auto' }}>
        {Array.from({ length: rings }).map((_, i) => {
          const r = (box / 2 - sw) * (1 - i / (rings + 0.4));
          return <ellipse key={i} cx={c} cy={c} rx={r * 1.04} ry={r * 0.9} fill="none" stroke={i === 0 ? 'var(--contour)' : 'var(--contour-line)'} strokeWidth={i === 0 ? sw : Math.max(1, sw - 0.25)} />;
        })}
        <circle cx={c} cy={c} r={Math.max(1.4, box * 0.075)} fill="var(--marker)" />
      </svg>
      {wordmark && (
        <span style={{ font: `500 ${Math.round(size * 1.15)}px/1 var(--font-display)`, fontOpticalSizing: 'auto', letterSpacing: 'var(--tracking-wordmark)', color: 'var(--ink)' }}>pando</span>
      )}
    </span>
  );
}
