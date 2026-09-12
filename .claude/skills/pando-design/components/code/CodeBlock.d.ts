import * as React from 'react';

export interface CodeLine {
  text: string;
  /** Leading timestamp for log streams. */
  time?: string;
  /** `ok` vegetation-deep · `fail` marker · `muted` 55% terminal-text. */
  tone?: 'default' | 'muted' | 'ok' | 'fail';
}

/**
 * Code block, log stream and terminal output — always ink with terminal-text in
 * both themes, 6px radius, 16px padding, copy button top-right.
 */
export interface CodeBlockProps extends React.HTMLAttributes<HTMLDivElement> {
  lines?: Array<CodeLine | string> | string;
  /** Renders a `$` prompt in contour brown before each line. For commands to run. */
  prompt?: boolean;
  /** Quiet caption inside the block, e.g. "Build log · 2 min ago". */
  title?: string;
  copyable?: boolean;
  dense?: boolean;
}

export declare function CodeBlock(props: CodeBlockProps): JSX.Element;
