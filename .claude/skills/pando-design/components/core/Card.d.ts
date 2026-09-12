import * as React from 'react';

/**
 * Bounded object: a 1px rule border at 6px radius, no shadow. Use it when the
 * thing inside is a single object — one app, one host, one deploy.
 */
export interface CardProps extends React.HTMLAttributes<HTMLElement> {
  tone?: 'paper' | 'sunken' | 'plain';
  padding?: 'none' | 'sm' | 'md' | 'lg';
  /** Border darkens to `--ink-secondary` on hover. For whole-card links. */
  interactive?: boolean;
  as?: keyof JSX.IntrinsicElements;
  children?: React.ReactNode;
}

export declare function Card(props: CardProps): JSX.Element;
