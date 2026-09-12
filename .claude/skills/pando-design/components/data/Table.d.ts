import * as React from 'react';

export interface TableColumn {
  key: string;
  header: string;
  /** CSS grid track, e.g. `1fr`, `120px`, `minmax(0,2fr)`. */
  width?: string;
  align?: 'left' | 'right';
  /** Mono (code-sm) cell type — commit hashes, IDs. */
  mono?: boolean;
  muted?: boolean;
  render?: (row: any) => React.ReactNode;
}

/** 40px rows (32px dense), 1px rule dividers, no zebra striping, tabular figures. */
export interface TableProps extends React.HTMLAttributes<HTMLDivElement> {
  columns?: TableColumn[];
  rows?: any[];
  dense?: boolean;
  onRowClick?: (row: any) => void;
  /** Rendered in place of rows when the list is empty — usually an `EmptyState`. */
  empty?: React.ReactNode;
}

export declare function Table(props: TableProps): JSX.Element;
