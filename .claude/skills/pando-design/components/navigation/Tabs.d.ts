import * as React from 'react';

export interface TabItem { value: string; label: string; trailing?: React.ReactNode }

/**
 * Switches sections within one screen. A 1px ink underline marks the active tab —
 * the same structural logic as every other rule in the system. An intentional
 * addition to the brand spec.
 */
export interface TabsProps extends React.HTMLAttributes<HTMLDivElement> {
  items?: TabItem[];
  value?: string;
  defaultValue?: string;
  onChange?: (value: string) => void;
}

export declare function Tabs(props: TabsProps): JSX.Element;
