import React from 'react';

/* The collar of a printed survey quadrangle: a neatline, corner ticks, and the
   marginal data set outside it.

   The spec defines the collar as part of the hero contour figure, and it lived
   inside ContourMap as literals. It is a separate idea from the contour itself
   — the contour is terrain, the collar is the sheet the terrain is printed on —
   and the console needs the sheet without the terrain, because the contour
   figure is restricted to four places and a page frame is not one of them.

   So it is its own component, and ContourMap composes it. One implementation of
   the tick geometry, not two. */
export function MapCollar({
  marginalia,
  scale = false,
  scaleLabels = ['0', '½', '1 mi'],
  inset = true,
  children,
  style,
  ...rest
}) {
  return (
    <div
      style={{
        position: 'relative',
        border: 'var(--border-width) solid var(--rule-strong)',
        padding: inset ? 'var(--space-3)' : 0,
        ...style,
      }}
      {...rest}
    >
      <Ticks />
      {marginalia && (
        <div
          style={{
            display: 'flex',
            justifyContent: 'space-between',
            font: 'var(--type-code-sm)',
            color: 'var(--ink-secondary)',
            padding: '0 var(--space-1) var(--space-2)',
          }}
        >
          <span>{marginalia[0]}</span>
          <span>{marginalia[1]}</span>
        </div>
      )}
      {children}
      {scale && (
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 'var(--space-2)',
            paddingTop: 'var(--space-3)',
          }}
        >
          <div style={{ display: 'flex' }}>
            {[0, 1, 2, 3].map((i) => (
              <span
                key={i}
                style={{
                  width: 12,
                  height: 4,
                  background: i % 2 === 0 ? 'var(--ink)' : 'var(--paper-raised)',
                  border: '1px solid var(--ink)',
                  borderLeft: i === 0 ? '1px solid var(--ink)' : 'none',
                }}
              />
            ))}
          </div>
          <span style={{ font: 'var(--type-caption)', color: 'var(--ink-secondary)' }}>
            {scaleLabels.join('    ')}
          </span>
        </div>
      )}
    </div>
  );
}

/* Eight-pixel L-marks sitting 3px outside each corner of the neatline, the way
   a quadrangle's corner ticks overrun the frame. Decoration with no reading, so
   they are hidden from assistive technology. */
function Ticks() {
  const t = {
    position: 'absolute',
    width: 8,
    height: 8,
    borderColor: 'var(--rule-strong)',
    borderStyle: 'solid',
    borderWidth: 0,
  };
  return (
    <React.Fragment>
      <span aria-hidden="true" style={{ ...t, top: -1, left: -1, borderTopWidth: 1, borderLeftWidth: 1, transform: 'translate(-3px,-3px)' }} />
      <span aria-hidden="true" style={{ ...t, top: -1, right: -1, borderTopWidth: 1, borderRightWidth: 1, transform: 'translate(3px,-3px)' }} />
      <span aria-hidden="true" style={{ ...t, bottom: -1, left: -1, borderBottomWidth: 1, borderLeftWidth: 1, transform: 'translate(-3px,3px)' }} />
      <span aria-hidden="true" style={{ ...t, bottom: -1, right: -1, borderBottomWidth: 1, borderRightWidth: 1, transform: 'translate(3px,3px)' }} />
    </React.Fragment>
  );
}
