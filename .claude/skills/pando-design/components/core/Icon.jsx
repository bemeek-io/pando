import React from 'react';

import { icons } from '../../assets/icons/index.js';

/**
 * Lucide outline icon, normalized to the brand's 1.5px stroke.
 *
 * Renders from the vendored set in `assets/icons/`, synchronously. It used to
 * `fetch` each glyph from the unpkg CDN on first render, which had two costs:
 * an installation on a private network got no icons at all — a normal way to
 * run Pando — and even online every icon appeared a moment after the rest of
 * the interface. The brand spec asked for exactly this change: "for production
 * or offline use, vendor the icons you actually use into `assets/icons/` and
 * point `Icon` at them."
 *
 * An unknown name renders nothing rather than throwing. A missing icon should
 * cost a gap in the chrome, not a blank screen — and the console's own lint
 * cannot catch a typo in a string.
 */
export function Icon({ name, size = 16, stroke = 1.5, color = 'var(--ink-secondary)', style, ...rest }) {
  const markup = icons[name];

  if (markup === undefined) {
    if (typeof console !== 'undefined' && console.warn) {
      console.warn(
        `[pando-design] no icon named "${name}". Add assets/icons/${name}.svg and re-run assets/icons/build.mjs.`,
      );
    }
  }

  return (
    <span
      aria-hidden="true"
      data-icon={name}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        justifyContent: 'center',
        flex: '0 0 auto',
        width: size,
        height: size,
        color,
        ...style,
      }}
      {...rest}
    >
      {markup !== undefined && (
        <svg
          width="100%"
          height="100%"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth={stroke}
          strokeLinecap="round"
          strokeLinejoin="round"
          dangerouslySetInnerHTML={{ __html: markup }}
        />
      )}
    </span>
  );
}
