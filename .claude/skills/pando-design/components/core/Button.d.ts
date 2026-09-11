import * as React from 'react';

/**
 * Action control. One primary button per view. Labels name the action in
 * sentence case ("Deploy", "Share app", "Roll back") and never take a trailing icon.
 */
export interface ButtonProps extends Omit<React.ButtonHTMLAttributes<HTMLButtonElement>, 'size'> {
  variant?: 'primary' | 'secondary' | 'ghost' | 'destructive';
  /** 32px console (default) or 40px marketing. */
  size?: 'console' | 'marketing';
  /** Leading icon only — nothing ever goes after the label. */
  icon?: React.ReactNode;
  disabled?: boolean;
  fullWidth?: boolean;
  children?: React.ReactNode;
}

export declare function Button(props: ButtonProps): JSX.Element;
