import * as React from 'react';

/** 2px-radius metadata chip: branch, region, runtime, scope. Never a status. */
export interface TagProps extends React.HTMLAttributes<HTMLSpanElement> {
  /** Mono for machine values (branches, IDs). */
  mono?: boolean;
  icon?: React.ReactNode;
  tone?: 'default' | 'contour' | 'vegetation';
  children?: React.ReactNode;
}

export declare function Tag(props: TagProps): JSX.Element;
