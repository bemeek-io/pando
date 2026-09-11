import React, { useState } from 'react';

export function Table({ columns = [], rows = [], dense = false, onRowClick, empty = null, style, ...rest }) {
  const grid = columns.map((c) => c.width || '1fr').join(' ');
  const h = dense ? 'var(--row-height-dense)' : 'var(--row-height)';
  return (
    <div style={{ width: '100%', ...style }} {...rest}>
      <div style={{ display: 'grid', gridTemplateColumns: grid, alignItems: 'center', gap: 'var(--space-4)', minHeight: dense ? 28 : 32, padding: '0 var(--space-3)', background: 'var(--paper-sunken)', borderTop: '1px solid var(--rule)', borderBottom: '1px solid var(--rule)' }}>
        {columns.map((c) => (
          <span key={c.key} style={{ font: 'var(--type-label)', color: 'var(--ink-secondary)', textAlign: c.align || 'left' }}>{c.header}</span>
        ))}
      </div>
      {rows.length === 0 && empty}
      {rows.map((r, i) => <Row key={r.id || i} row={r} columns={columns} grid={grid} height={h} onRowClick={onRowClick} />)}
    </div>
  );
}

function Row({ row, columns, grid, height, onRowClick }) {
  const [hover, setHover] = useState(false);
  return (
    <div
      onClick={onRowClick ? () => onRowClick(row) : undefined}
      onMouseEnter={() => setHover(true)} onMouseLeave={() => setHover(false)}
      style={{
        display: 'grid', gridTemplateColumns: grid, alignItems: 'center', gap: 'var(--space-4)',
        minHeight: height, padding: '0 var(--space-3)',
        borderBottom: '1px solid var(--rule)',
        background: hover && onRowClick ? 'var(--paper-raised)' : 'transparent',
        cursor: onRowClick ? 'pointer' : undefined,
        transition: 'background-color var(--dur-fast) var(--ease)',
      }}
    >
      {columns.map((c) => (
        <div key={c.key} style={{ font: c.mono ? 'var(--type-code-sm)' : 'var(--type-body-ui)', color: c.muted ? 'var(--ink-secondary)' : 'var(--ink)', textAlign: c.align || 'left', justifySelf: c.align === 'right' ? 'end' : undefined, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
          {c.render ? c.render(row) : row[c.key]}
        </div>
      ))}
    </div>
  );
}
