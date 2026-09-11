import React from 'react';

export function SidebarNav({ items = [], value, onChange, header, footer, style, ...rest }) {
  return (
    <nav
      style={{
        width: 'var(--console-sidebar)', flex: '0 0 auto',
        display: 'flex', flexDirection: 'column',
        background: 'var(--paper)', borderRight: '1px solid var(--rule)',
        ...style,
      }}
      {...rest}
    >
      {header && <div style={{ padding: 'var(--space-4)' }}>{header}</div>}
      <div style={{ display: 'flex', flexDirection: 'column', padding: '0 var(--space-2)', gap: 2 }}>
        {items.map((it) => {
          const on = it.value === value;
          return (
            <button
              key={it.value} onClick={() => onChange && onChange(it.value)}
              style={{
                display: 'flex', alignItems: 'center', gap: 'var(--space-2)',
                height: 'var(--control-console)', padding: '0 var(--space-2)',
                border: '1px solid transparent', borderRadius: 'var(--radius-sm)',
                background: on ? 'var(--paper-sunken)' : 'transparent',
                color: on ? 'var(--ink)' : 'var(--ink-secondary)',
                font: on ? 'var(--type-label)' : 'var(--type-body-ui)',
                textAlign: 'left', cursor: 'pointer',
                transition: 'background-color var(--dur-fast) var(--ease), color var(--dur-fast) var(--ease)',
              }}
            >
              {it.icon}
              <span style={{ flex: 1 }}>{it.label}</span>
              {it.trailing}
            </button>
          );
        })}
      </div>
      {footer && <div style={{ marginTop: 'auto', padding: 'var(--space-4)', borderTop: '1px solid var(--rule)' }}>{footer}</div>}
    </nav>
  );
}
