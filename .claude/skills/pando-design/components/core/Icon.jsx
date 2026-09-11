import React from 'react';

const CDN = 'https://unpkg.com/lucide-static@0.469.0/icons/';
const cache = {};

/** Lucide outline icon, normalized to the brand's 1.5px stroke. */
export function Icon({ name, size = 16, stroke = 1.5, color = 'var(--ink-secondary)', style, ...rest }) {
  const [svg, setSvg] = React.useState(cache[name] || null);
  React.useEffect(() => {
    if (cache[name]) { setSvg(cache[name]); return; }
    let live = true;
    fetch(CDN + name + '.svg')
      .then((r) => (r.ok ? r.text() : Promise.reject(r.status)))
      .then((t) => { cache[name] = t; if (live) setSvg(t); })
      .catch(() => {});
    return () => { live = false; };
  }, [name]);
  const markup = svg
    ? svg.replace(/width="24"/, 'width="100%"').replace(/height="24"/, 'height="100%"').replace(/stroke-width="[\d.]+"/, 'stroke-width="' + stroke + '"')
    : '';
  return (
    <span
      aria-hidden="true"
      data-icon={name}
      style={{ display: 'inline-flex', alignItems: 'center', justifyContent: 'center', flex: '0 0 auto', width: size, height: size, color, ...style }}
      dangerouslySetInnerHTML={{ __html: markup }}
      {...rest}
    />
  );
}
