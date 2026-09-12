import * as React from 'react';

/**
 * App status: a distinct symbol plus a text label, so status never relies on
 * color alone. Filled circle running · hollow ring building · filled triangle
 * failed · short dash stopped · filled circle info.
 */
export interface StatusIndicatorProps extends React.HTMLAttributes<HTMLSpanElement> {
  status?: 'running' | 'building' | 'failed' | 'stopped' | 'info';
  /** Overrides the default sentence-case label. */
  label?: string;
  /** Symbol size in px. 8 is standard. */
  size?: number;
}

export interface StatusSymbolProps { status?: StatusIndicatorProps['status']; size?: number; style?: React.CSSProperties }

export declare function StatusIndicator(props: StatusIndicatorProps): JSX.Element;
export declare function StatusSymbol(props: StatusSymbolProps): JSX.Element;
