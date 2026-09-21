import { describe, expect, it } from 'vitest';

import { tileOf } from './TopoBackground';

// R-340: an app with no uploaded image gets a picture generated from its ID —
// the same every time, and different for every app.
describe('R-340 generated app tiles', () => {
  it('draws the same terrain for the same app every time', () => {
    const a = tileOf('app_01M2GQJ2Q7G5AT10FQJS5CFGA0');
    const b = tileOf('app_01M2GQJ2Q7G5AT10FQJS5CFGA0');
    expect(b.levels.join('')).toBe(a.levels.join(''));
    expect(b.sheet).toEqual(a.sheet);
  });

  it('draws different terrain for every app', () => {
    const ids = Array.from({ length: 50 }, (_, n) => `app_01M2GQJ2Q7G5AT10FQJS5C${String(n).padStart(4, '0')}`);
    const drawn = new Set(ids.map((id) => tileOf(id).levels.join('')));
    expect(drawn.size).toBe(ids.length);
  });

  it('draws actual contours', () => {
    const { levels } = tileOf('app_01M2E0M27R4VJD5Y03DEN8468B');
    expect(levels.length).toBeGreaterThanOrEqual(7);
    expect(levels.filter((d) => d.length > 0).length).toBeGreaterThan(3);
  });
});
