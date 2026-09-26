import { describe, expect, it } from 'vitest';

import { phrasesFor } from './onboardingPhrases';

const STEPS = ['read', 'stack', 'runs', 'vars', 'trial', 'repair', 'answer'];

describe('phrasesFor', () => {
  it('has phrases for every step, and falls back for an unknown one', () => {
    for (const step of [...STEPS, undefined, 'mystery']) {
      expect(phrasesFor(step).length).toBeGreaterThan(0);
    }
    expect(phrasesFor('mystery')).toEqual(phrasesFor('read'));
  });

  it('keeps to the voice rules: sentence case, no exclamation marks, no dots of their own', () => {
    for (const step of STEPS) {
      for (const phrase of phrasesFor(step)) {
        expect(phrase).not.toContain('!');
        expect(phrase.endsWith('…') || phrase.endsWith('.')).toBe(false);
        expect(phrase.charAt(0)).toBe(phrase.charAt(0).toUpperCase());
      }
    }
  });
});
