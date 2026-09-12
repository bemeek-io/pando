import * as React from 'react';

/**
 * A 120px contour with its summit mark, an inviting h3, one sentence, one primary button.
 */
export interface EmptyStateProps extends React.HTMLAttributes<HTMLDivElement> {
  /** Invites the action: "Deploy your first app". */
  heading: string;
  /** One sentence of explanation. */
  children?: React.ReactNode;
  /** One primary Button. */
  action?: React.ReactNode;
  /** Contour size. 120px is standard. */
  size?: number;
}

export declare function EmptyState(props: EmptyStateProps): JSX.Element;
