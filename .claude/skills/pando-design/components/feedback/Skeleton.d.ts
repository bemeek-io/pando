import * as React from 'react';

/**
 * The shape of something still loading: a static block on paper-sunken. No
 * shimmer or pulse — the motion rules allow no load animations. Hidden for its
 * first `delay` ms, with its space kept, so a fast load never flashes it.
 */
export interface SkeletonProps extends React.HTMLAttributes<HTMLSpanElement> {
  /** A CSS length. Defaults to the full width. */
  width?: string;
  /** A CSS length. Defaults to one line of text, 1em. */
  height?: string;
  /** Radius token: xs, sm, md, lg. The real thing's radius, so the shape matches. */
  radius?: 'xs' | 'sm' | 'md' | 'lg';
  /** Milliseconds before it shows. 150 by default; 0 shows at once. */
  delay?: number;
}

export declare function Skeleton(props: SkeletonProps): JSX.Element;

/** Lines of text still loading, the last one shorter. Announced as "Loading". */
export interface SkeletonTextProps extends React.HTMLAttributes<HTMLSpanElement> {
  lines?: number;
  delay?: number;
}

export declare function SkeletonText(props: SkeletonTextProps): JSX.Element;
