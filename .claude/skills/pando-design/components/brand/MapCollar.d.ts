import * as React from 'react';

/**
 * The neatline of a printed survey sheet: a 1px `--rule-strong` frame with 8px
 * corner ticks, optional mono marginalia across the top and an optional
 * four-segment scale bar beneath.
 *
 * **Intentional addition.** The brand spec describes the collar only as part of
 * the hero contour figure, where it was hard-coded inside `ContourMap`. It is
 * pulled out because the collar and the contour are different ideas: the
 * contour is terrain and is restricted to four places, while the collar is the
 * sheet, and framing a page as a sheet is the topo language's quiet half.
 * `ContourMap` composes this, so the tick geometry has one definition.
 *
 * Use it to frame a page or a bounded region. It is structure, not
 * illustration, so it does not count as the screen's one bold element — the
 * spec's own hero prints a contour inside a collar.
 *
 * Do not put a collar inside a collar, and do not use it on a card: a card is
 * already a bounded object, and two frames around one thing is two frames.
 */
export interface MapCollarProps extends React.HTMLAttributes<HTMLDivElement> {
  /** Two short mono strings set at the top corners, printed-map style. Coordinates on the hero. */
  marginalia?: [string, string];
  /** Render the four-segment scale bar beneath the content. Hero scale only. */
  scale?: boolean;
  /** Labels under the scale bar. */
  scaleLabels?: string[];
  /** Pad the content away from the neatline. Off when the content brings its own padding. */
  inset?: boolean;
  children?: React.ReactNode;
}

export declare function MapCollar(props: MapCollarProps): JSX.Element;
