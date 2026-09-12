import * as React from 'react';

/** Lucide (or Tabler) outline icon at the brand's 1.5px stroke weight. */
export interface IconProps extends React.HTMLAttributes<HTMLSpanElement> {
  /** Lucide slug, kebab-case: `circle-check`, `git-branch`, `copy`. */
  name: string;
  /** 16px in the console, 20px on marketing. */
  size?: number;
  /** Stroke width. Leave at 1.5. */
  stroke?: number;
  /** Defaults to `--ink-secondary`. */
  color?: string;
}

export declare function Icon(props: IconProps): JSX.Element;
