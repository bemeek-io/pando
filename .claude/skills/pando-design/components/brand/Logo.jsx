import React from 'react';

/**
 * The Pando logo: "pando." with the full stop in marker red.
 *
 * This is the real artwork, replacing the contour-and-summit construction that
 * stood here before. PROVENANCE.md anticipated it — "the logo in Logo.jsx is
 * built from the spec's written description, not from official artwork; if real
 * logo files exist, they replace it" — and they now do, in assets/logo/.
 *
 * Drawn from tokens rather than loading the SVG, for two reasons. The exports
 * hard-code #1A1C1B and #B23A2C, which are exactly `--ink` and `--marker`, so
 * rendering from tokens follows the theme automatically instead of needing a
 * light file and a dark file. And the SVGs carry live text in Newsreader, so
 * they depend on the same font this does and gain nothing by being files.
 *
 * The wordmark is the lockup. `wordmark={false}` gives the icon — the ink tile
 * with "p." in it — for a favicon, an avatar or dense chrome.
 */
export function Logo({ size = 24, wordmark = true, style, ...rest }) {
  if (!wordmark) return <LogoMark size={size} style={style} {...rest} />;

  // 128px type on a 160px-tall export: the wordmark's cap height is 0.8 of the
  // nominal size, and `size` names the mark's height as it does elsewhere.
  const fontSize = Math.round(size * 1.25);

  return (
    <span
      aria-label="Pando"
      role="img"
      style={{
        display: 'inline-flex',
        alignItems: 'baseline',
        font: `500 ${fontSize}px/1 var(--font-display)`,
        fontOpticalSizing: 'auto',
        letterSpacing: 'var(--tracking-wordmark)',
        color: 'var(--ink)',
        ...style,
      }}
      {...rest}
    >
      pando
      {/* The one piece of marker red in the chrome, and the whole mark's
          character. Never tinted, never dropped. */}
      <span style={{ color: 'var(--marker)' }}>.</span>
    </span>
  );
}

/**
 * The icon: "p." in paper on an ink tile.
 *
 * Proportions from the export — a 512px tile at radius 112, the glyph centred
 * at 366 on the baseline with the same letter-spacing as the wordmark. Kept as
 * a viewBox so one component serves 16px chrome and a 512px app icon.
 */
function LogoMark({ size = 24, style, ...rest }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 512 512"
      role="img"
      aria-label="Pando"
      style={{ display: 'block', flex: '0 0 auto', ...style }}
      {...rest}
    >
      <rect width="512" height="512" rx="112" fill="var(--ink)" />
      <text
        x="256"
        y="366"
        textAnchor="middle"
        fontFamily="var(--font-display)"
        fontWeight="600"
        fontSize="352"
        letterSpacing="-7"
        fill="var(--paper)"
      >
        p<tspan fill="var(--marker)">.</tspan>
      </text>
    </svg>
  );
}
