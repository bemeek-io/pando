import * as React from 'react';

/** Full-width message: status tint, 1px status border, the status symbol, one sentence, at most one action. */
export interface BannerProps extends React.HTMLAttributes<HTMLDivElement> {
  tone?: 'info' | 'running' | 'building' | 'failed';
  /** At most one action — a ghost or secondary Button. */
  action?: React.ReactNode;
  children?: React.ReactNode;
}

export declare function Banner(props: BannerProps): JSX.Element;
