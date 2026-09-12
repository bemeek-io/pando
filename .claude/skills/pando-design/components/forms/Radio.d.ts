import * as React from 'react';

/** One radio input. Group two or three with a shared `name` so the tradeoff is visible. */
export interface RadioProps extends Omit<React.InputHTMLAttributes<HTMLInputElement>, 'type'> {
  label?: string;
  description?: string;
  checked?: boolean;
  name?: string;
  value?: string;
}

export declare function Radio(props: RadioProps): JSX.Element;
