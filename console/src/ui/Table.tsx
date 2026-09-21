// The design system's Table, with the one hook narrow.css needs.
//
// A class, so that at phone width a table can scroll sideways inside itself
// rather than crushing its columns (see narrow.css). Everything else is the
// design system's own component, passed through untouched.

import { Table as DesignTable } from '@design';
import type { TableProps } from '@design';

export function Table({ className, ...props }: TableProps) {
  return <DesignTable className={className ? `pando-table ${className}` : 'pando-table'} {...props} />;
}
