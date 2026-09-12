import * as React from 'react';

export interface SelectOption { value: string; label: string }

/** Native select with Pando field chrome. For four or more options; two or three use Radio. */
export interface SelectProps extends React.SelectHTMLAttributes<HTMLSelectElement> {
  label?: string;
  helper?: string;
  options?: Array<SelectOption | string>;
  mono?: boolean;
}

export declare function Select(props: SelectProps): JSX.Element;
