import { describe, expect, it } from 'vitest';

import { bytes, cores } from './usage-format';

describe('usage numbers', () => {
  it('reads CPU in cores', () => {
    expect(cores(1000)).toBe('1.00 core');
    expect(cores(150)).toBe('0.15 cores');
    expect(cores(4)).toBe('0.004 cores');
    expect(cores(0)).toBe('0.00 cores');
  });

  it('reads sizes as people do', () => {
    expect(bytes(512)).toBe('512 B');
    expect(bytes(64 << 10)).toBe('64 KB');
    expect(bytes(256 << 20)).toBe('256.0 MB');
    expect(bytes(3 * (1 << 30))).toBe('3.0 GB');
  });
});
