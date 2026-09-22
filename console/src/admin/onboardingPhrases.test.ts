import { describe, expect, it } from 'vitest';

import { GLYPHS, phrasesFor } from './onboardingPhrases';

const STAGES = ['fetching', 'detecting', 'trying', 'screening'];

describe('phrasesFor', () => {
  it('has phrases for every stage, and falls back for an unknown one', () => {
    for (const stage of [...STAGES, undefined, 'mystery']) {
      expect(phrasesFor(stage).length).toBeGreaterThan(0);
    }
    expect(phrasesFor('mystery')).toEqual(phrasesFor('fetching'));
  });

  it('keeps to the voice rules: present tense with an ellipsis, no exclamation marks', () => {
    for (const stage of STAGES) {
      for (const phrase of phrasesFor(stage)) {
        expect(phrase.endsWith('…')).toBe(true);
        expect(phrase).not.toContain('!');
        expect(phrase.charAt(0)).toBe(phrase.charAt(0).toUpperCase());
      }
    }
  });

  it('turns through more than one glyph', () => {
    expect(GLYPHS.length).toBeGreaterThan(1);
  });
});
