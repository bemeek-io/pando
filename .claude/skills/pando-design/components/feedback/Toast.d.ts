import * as React from 'react';

/**
 * Confirms something that already happened, in one sentence, bottom-right, one at
 * a time. An intentional addition to the brand spec — it follows the banner's
 * symbol-plus-sentence rule on a paper-raised surface.
 */
export interface ToastProps extends React.HTMLAttributes<HTMLDivElement> {
  status?: 'running' | 'building' | 'failed' | 'info';
  /** At most one ghost Button. */
  action?: React.ReactNode;
  onDismiss?: () => void;
  children?: React.ReactNode;
}

export declare function Toast(props: ToastProps): JSX.Element;
