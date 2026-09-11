import React from 'react';
import { StatusSymbol } from '../data/StatusIndicator.jsx';

const TONES = {
  info: { bg: 'var(--status-info-tint)', border: 'var(--status-info)', status: 'info', color: 'var(--ink)' },
  running: { bg: 'var(--status-running-tint)', border: 'var(--status-running)', status: 'running', color: 'var(--ink)' },
  building: { bg: 'var(--status-building-tint)', border: 'var(--status-building)', status: 'building', color: 'var(--ink)' },
  failed: { bg: 'var(--status-failed-tint)', border: 'var(--status-failed)', status: 'failed', color: 'var(--marker-deep)' },
};

export function Banner({ tone = 'info', action, style, children, ...rest }) {
  const t = TONES[tone] || TONES.info;
  return (
    <div
      role={tone === 'failed' ? 'alert' : 'status'}
      style={{
        display: 'flex', alignItems: 'center', gap: 'var(--space-3)',
        width: '100%', padding: 'var(--space-3) var(--space-4)',
        background: t.bg, border: '1px solid ' + t.border, borderRadius: 'var(--radius-sm)',
        font: 'var(--type-body-ui)', color: t.color,
        ...style,
      }}
      {...rest}
    >
      <StatusSymbol status={t.status} size={8} style={{ marginTop: 1 }} />
      <span style={{ flex: 1, maxWidth: 'none' }}>{children}</span>
      {action}
    </div>
  );
}
