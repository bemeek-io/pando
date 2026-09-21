// Type declarations for the import surface.
//
// Written here, like `index.js` itself: the design project ships per-component
// `.d.ts` files but no barrel, and the adherence config requires consumers to
// import from one. Without this the console would import the barrel as `any`
// and lose every prop type the 24 declaration files provide — which is the
// checking the adherence config exists to enforce.
//
// Keep in step with `index.js`. A component exported there and missing here
// types as an implicit any at the call site.

export { ContourMap } from './components/brand/ContourMap';
export type { ContourMapProps } from './components/brand/ContourMap';
export { Logo } from './components/brand/Logo';
export type { LogoProps } from './components/brand/Logo';
export { MapCollar } from './components/brand/MapCollar';
export type { MapCollarProps } from './components/brand/MapCollar';

export { CodeBlock } from './components/code/CodeBlock';
export type { CodeBlockProps, CodeLine } from './components/code/CodeBlock';
export { InlineCode } from './components/code/InlineCode';
export type { InlineCodeProps } from './components/code/InlineCode';

export { Badge } from './components/core/Badge';
export type { BadgeProps } from './components/core/Badge';
export { Button } from './components/core/Button';
export type { ButtonProps } from './components/core/Button';
export { Card } from './components/core/Card';
export type { CardProps } from './components/core/Card';
export { Icon } from './components/core/Icon';
export type { IconProps } from './components/core/Icon';
export { IconButton } from './components/core/IconButton';
export type { IconButtonProps } from './components/core/IconButton';
export { Tag } from './components/core/Tag';
export type { TagProps } from './components/core/Tag';

export { StatusIndicator, StatusSymbol } from './components/data/StatusIndicator';
export type { StatusIndicatorProps, StatusSymbolProps } from './components/data/StatusIndicator';
export { Table } from './components/data/Table';
export type { TableProps, TableColumn } from './components/data/Table';

export { Banner } from './components/feedback/Banner';
export type { BannerProps } from './components/feedback/Banner';
export { Dialog } from './components/feedback/Dialog';
export type { DialogProps } from './components/feedback/Dialog';
export { EmptyState } from './components/feedback/EmptyState';
export type { EmptyStateProps } from './components/feedback/EmptyState';
export { Toast } from './components/feedback/Toast';
export type { ToastProps } from './components/feedback/Toast';
export { Tooltip } from './components/feedback/Tooltip';
export type { TooltipProps } from './components/feedback/Tooltip';

export { Checkbox } from './components/forms/Checkbox';
export type { CheckboxProps } from './components/forms/Checkbox';
export { Input } from './components/forms/Input';
export type { InputProps } from './components/forms/Input';
export { Radio } from './components/forms/Radio';
export type { RadioProps } from './components/forms/Radio';
export { Select } from './components/forms/Select';
export type { SelectProps, SelectOption } from './components/forms/Select';
export { Switch } from './components/forms/Switch';
export type { SwitchProps } from './components/forms/Switch';

export { SidebarNav } from './components/navigation/SidebarNav';
export type { SidebarNavProps, SidebarItem } from './components/navigation/SidebarNav';
export { Tabs } from './components/navigation/Tabs';
export type { TabsProps, TabItem } from './components/navigation/Tabs';
