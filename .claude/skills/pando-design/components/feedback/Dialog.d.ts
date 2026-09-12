import * as React from 'react';

/** Modal for a decision that must be made now. 8px radius, popover shadow, ink scrim at 40%. */
export interface DialogProps extends React.HTMLAttributes<HTMLDivElement> {
  open?: boolean;
  title?: string;
  /** One line saying what will happen. */
  description?: string;
  /** Right-aligned actions above a 1px rule. */
  footer?: React.ReactNode;
  width?: number;
  onClose?: () => void;
}

export declare function Dialog(props: DialogProps): JSX.Element;
