import * as React from 'react';

/**
 * The brand's signature illustration: closed, irregular, roughly concentric
 * contour rings with exactly one red summit mark. Every fifth ring is an index
 * contour (1.5px `--contour`); the rest are 1px `--contour-line`.
 */
export interface ContourMapProps extends React.HTMLAttributes<HTMLElement> {
  /** Rendered width in px. 480-560 hero · 320 docs header · 120 empty state. */
  size?: number;
  /** Ring count. Defaults to 8 at hero scale, 4 below 320px. 5-9 hero, 3-5 small. */
  rings?: number;
  /** Map collar: 1px frame, corner ticks, coordinates, scale bar. Hero only. */
  collar?: boolean;
  /** One summit mark per figure. Turn off only for the 404 page. */
  summit?: boolean;
  /** Draw the rings in from the outside over 900ms, then the summit. First load of the marketing hero only. */
  animate?: boolean;
  /** Collar coordinates — defaults to Pando's actual location. */
  coordinates?: [string, string];
  /** Elevation label broken into one index contour. */
  elevation?: string;
  scaleLabels?: string[];
}

export declare function ContourMap(props: ContourMapProps): JSX.Element;
