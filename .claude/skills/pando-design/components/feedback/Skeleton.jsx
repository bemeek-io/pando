import React, { useEffect, useState } from 'react';

// The shape of something still loading: a block on paper-sunken, where the
// real thing will be.
//
// Static. The motion rules allow no load animations but the hero contour's
// draw-in, so there is no shimmer and no pulse — the shape alone says
// "something goes here", and it does not flicker for the reader to watch.
//
// Held back for a moment (`delay`, 150ms): a load that finishes first shows
// nothing at all rather than a flash of grey. The space is kept from the
// start, hidden, so the page does not jump when it appears.
export function Skeleton({ width = '100%', height = '1em', radius = 'sm', delay = 150, style, ...rest }) {
  const [shown, setShown] = useState(delay <= 0);
  useEffect(() => {
    if (delay <= 0) return undefined;
    const t = setTimeout(() => setShown(true), delay);
    return () => clearTimeout(t);
  }, [delay]);

  return (
    <span
      aria-hidden="true"
      style={{
        display: 'block',
        width,
        height,
        maxWidth: '100%',
        background: 'var(--paper-sunken)',
        borderRadius: `var(--radius-${radius})`,
        visibility: shown ? 'visible' : 'hidden',
        ...style,
      }}
      {...rest}
    />
  );
}

// Lines of text still loading: the last one shorter, as a paragraph's is.
export function SkeletonText({ lines = 3, delay = 150, style, ...rest }) {
  return (
    <span
      role="status"
      aria-label="Loading"
      style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-2)', ...style }}
      {...rest}
    >
      {Array.from({ length: lines }, (_, i) => (
        <Skeleton key={i} delay={delay} width={i === lines - 1 && lines > 1 ? '60%' : '100%'} />
      ))}
    </span>
  );
}
