import * as React from 'react';

/** Pando lockup: contour mark + lowercase "pando" in Newsreader 500. */
export interface LogoProps extends React.HTMLAttributes<HTMLSpanElement> {
  /** Mark height in px. Minimum wordmark size is 16px tall. Two rings below 24px. */
  size?: number;
  /** Set false for the mark on its own (favicon, avatar, dense chrome). */
  wordmark?: boolean;
}

export declare function Logo(props: LogoProps): JSX.Element;
