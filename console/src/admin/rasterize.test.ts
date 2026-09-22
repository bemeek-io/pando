import { describe, expect, it } from 'vitest';

import { fit, isSVG, viewBoxSize } from './rasterize';

describe('rasterizing an SVG launcher image', () => {
  it('knows an SVG by type or by name', () => {
    expect(isSVG(new File([''], 'logo.svg'))).toBe(true);
    expect(isSVG(new File([''], 'logo', { type: 'image/svg+xml' }))).toBe(true);
    expect(isSVG(new File([''], 'logo.png', { type: 'image/png' }))).toBe(false);
  });

  it('fits a wide picture inside the square, centered', () => {
    expect(fit(200, 100, 512)).toEqual({ x: 0, y: 128, w: 512, h: 256 });
    expect(fit(50, 100, 512)).toEqual({ x: 128, y: 0, w: 256, h: 512 });
    expect(fit(0, 0, 512)).toEqual({ x: 0, y: 0, w: 512, h: 512 });
  });

  it('reads a size from the viewBox when there is no width or height', () => {
    expect(viewBoxSize('<svg viewBox="0 0 24 12">')).toEqual({ w: 24, h: 12 });
    expect(viewBoxSize("<svg viewBox='-2 -2 100,50'>")).toEqual({ w: 100, h: 50 });
    expect(viewBoxSize('<svg width="10">')).toBeNull();
  });
});
