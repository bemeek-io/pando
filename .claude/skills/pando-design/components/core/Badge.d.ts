import * as React from 'react';

/**
 * Small count next to a label (sidebar item, tab). Counts only — app state uses
 * `StatusIndicator`, because the brand never puts status in a filled pill.
 */
export interface BadgeProps extends React.HTMLAttributes<HTMLSpanElement> {
  count?: number;
  children?: React.ReactNode;
}

export declare function Badge(props: BadgeProps): JSX.Element;
