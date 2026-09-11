import React from 'react';

const STATUS = {
  running: { label: 'Running', color: 'var(--status-running)', shape: 'circle' },
  building: { label: 'Building', color: 'var(--status-building)', shape: 'ring' },
  failed: { label: 'Failed', color: 'var(--status-failed)', shape: 'triangle' },
  stopped: { label: 'Stopped', color: 'var(--status-stopped)', shape: 'dash' },
  info: { label: 'Info', color: 'var(--status-info)', shape: 'circle' },
};

export function StatusSymbol({ status = 'running', size = 8, style }) {
  const s = STATUS[status] || STATUS.info;
  const box = size + 2;
  return (
    <svg width={box} height={box} viewBox={`0 0 ${box} ${box}`} aria-hidden="true" style={{ display: 'block', flex: '0 0 auto', ...style }}>
      {s.shape === 'circle' && <circle cx={box / 2} cy={box / 2} r={size / 2} fill={s.color} />}
      {s.shape === 'ring' && <circle cx={box / 2} cy={box / 2} r={size / 2 - 0.6} fill="none" stroke={s.color} strokeWidth="1.5" />}
      {s.shape === 'triangle' && <polygon points={`${box / 2},${box / 2 - size / 2} ${box / 2 + size / 2},${box / 2 + size / 2.6} ${box / 2 - size / 2},${box / 2 + size / 2.6}`} fill={s.color} />}
      {s.shape === 'dash' && <rect x={box / 2 - size / 2} y={box / 2 - 0.75} width={size} height="1.5" fill={s.color} />}
    </svg>
  );
}

export function StatusIndicator({ status = 'running', label, size = 8, style, ...rest }) {
  const s = STATUS[status] || STATUS.info;
  return (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 'var(--space-2)', font: 'var(--type-body-ui)', color: 'var(--ink)', ...style }} {...rest}>
      <StatusSymbol status={status} size={size} />
      {label || s.label}
    </span>
  );
}
