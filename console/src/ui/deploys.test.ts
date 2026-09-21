import { describe, expect, it } from 'vitest';

import { deployLabel, deployStatus } from './deploys';

describe('how a deploy reads in a list', () => {
  it('says when the app it started never came up', () => {
    // The deploy worked: built, applied, routed. The app did not report
    // healthy, and the history said "Deployed" three times for an app that had
    // never served a request.
    expect(deployLabel('succeeded', 'degraded')).toBe('Deployed, not healthy');
    expect(deployStatus('succeeded', 'degraded')).toBe('building');
  });

  it('says plainly when it did', () => {
    expect(deployLabel('succeeded', 'running')).toBe('Deployed');
    expect(deployStatus('succeeded', 'running')).toBe('running');
  });

  // Deploys recorded before the result was kept carry nothing. Reading that as
  // "not healthy" would repaint an app's whole history on an upgrade.
  it('reads a deploy with no recorded result as it always did', () => {
    expect(deployLabel('succeeded')).toBe('Deployed');
    expect(deployStatus('succeeded')).toBe('running');
  });

  it('leaves the failures alone', () => {
    expect(deployLabel('failed')).toBe('Failed');
    expect(deployStatus('failed')).toBe('failed');
    expect(deployLabel('rolled_back')).toBe('Rolled back');
    expect(deployLabel('running')).toBe('Deploying');
  });
});
