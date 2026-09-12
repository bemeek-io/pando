import * as React from 'react';

/**
 * Icon-only control for chrome where a label doesn't fit: code-block copy button,
 * table row menus, dialog dismiss. An intentional addition to the brand spec.
 */
export interface IconButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  /** Required — becomes aria-label and the tooltip. */
  label: string;
  /** Square size in px. 28 console, 32 marketing. */
  size?: number;
  variant?: 'ghost' | 'secondary';
  /** Re-tints for use on the terminal surface. */
  onTerminal?: boolean;
  disabled?: boolean;
}

export declare function IconButton(props: IconButtonProps): JSX.Element;
