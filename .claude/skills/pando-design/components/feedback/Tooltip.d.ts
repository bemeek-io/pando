import * as React from 'react';

/** Hover label carrying the precise value behind a relative time, a truncated ID or an icon-only control. */
export interface TooltipProps extends React.HTMLAttributes<HTMLSpanElement> {
  content: React.ReactNode;
  side?: 'top' | 'bottom' | 'left' | 'right';
  children?: React.ReactNode;
}

export declare function Tooltip(props: TooltipProps): JSX.Element;
