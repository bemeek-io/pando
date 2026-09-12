import * as React from 'react';

/** 36px text field on paper-raised with a field-border rule. Label above, helper or error below. */
export interface InputProps extends React.InputHTMLAttributes<HTMLInputElement> {
  label?: string;
  /** Caption-size hint below the field. */
  helper?: string;
  /** Says what happened and how to fix it. Renders in marker-deep and reds the border. */
  error?: string;
  /** Mono type for commands, paths and IDs. */
  mono?: boolean;
  /** Render a textarea instead. */
  as?: 'input' | 'textarea';
  rows?: number;
}

export declare function Input(props: InputProps): JSX.Element;
