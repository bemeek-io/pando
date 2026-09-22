// The loading shapes the console repeats, built from the design system's
// Skeleton so each screen does not size its own.
//
// A skeleton stands where the real thing will be and takes its space, so
// nothing moves when the data arrives. A line of text is the case that needs
// help: Skeleton is 1em tall, a line of text is its line height tall, and the
// difference is the jump. So a line is a Skeleton inside a box set in the same
// type token, which gives it the real line box.

import { Skeleton } from '@design';

/** One line of text still loading, in the type it will be set in. */
export function LineSkeleton({ width = '24ch', font = 'var(--type-body-ui)' }: { width?: string; font?: string }) {
  return (
    // A span, so it can stand inside a line of other text as well as on its own.
    <span aria-hidden="true" style={{ display: 'block', font }}>
      <Skeleton width={width} style={{ display: 'inline-block', verticalAlign: 'middle' }} />
    </span>
  );
}

/** A page heading still loading: an h3's line, which is what Sheet sets. */
export function HeadingSkeleton({ width = '18ch' }: { width?: string }) {
  return <LineSkeleton width={width} font="var(--type-h3)" />;
}

/** A form field still loading: an input's height, the field's full width. */
export function FieldSkeleton({ width = '100%' }: { width?: string }) {
  return <Skeleton width={width} height="var(--control-input)" />;
}

/**
 * A block of loading content, announced once. Skeletons themselves are
 * aria-hidden; this is what tells a screen reader the page is still coming.
 */
export function Loading({ gap = 'var(--space-3)', children }: { gap?: string; children: React.ReactNode }) {
  return (
    <div role="status" aria-label="Loading" style={{ display: 'flex', flexDirection: 'column', gap }}>
      {children}
    </div>
  );
}
