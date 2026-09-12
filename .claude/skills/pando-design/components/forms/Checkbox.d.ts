import * as React from 'react';

/** 16px checkbox on paper-raised with a field-border rule, ink fill when checked. */
export interface CheckboxProps extends Omit<React.InputHTMLAttributes<HTMLInputElement>, 'type'> {
  label?: string;
  /** One caption line on the consequence of the setting. */
  description?: string;
  checked?: boolean;
  indeterminate?: boolean;
}

export declare function Checkbox(props: CheckboxProps): JSX.Element;
