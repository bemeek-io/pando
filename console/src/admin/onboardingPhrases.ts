// The status line on the onboarding page while detection runs.
//
// A present-tense phrase per stage, rotating while the stage lasts, in the
// brand's own vocabulary: Pando reads a repository the way a surveyor reads
// ground. Plain words, no jokes and no exclamation marks — the voice rules hold
// for a line that changes every few seconds as much as for an error.
//
// The first phrase of each stage says literally what Pando is doing, because
// that is the one shown under prefers-reduced-motion, where the line does not
// rotate.

const PHRASES: Record<string, string[]> = {
  fetching: ['Fetching the repository…', 'Unrolling the map…', 'Reading the file tree…'],
  detecting: [
    'Surveying the repository…',
    'Taking bearings…',
    'Weighing what this could be…',
    'Reading the landmarks…',
  ],
  trying: [
    'Trying a first run…',
    'Walking the ground…',
    'Watching which ports it opens…',
    'Checking what it writes…',
  ],
  screening: ['Having a second reader look…', 'Cross-checking the plan…', 'Plotting the contours…'],
};

/** The phrases for a stage. A stage this console does not know reads as the first. */
export function phrasesFor(stage: string | undefined): string[] {
  return PHRASES[stage ?? ''] ?? PHRASES.fetching!;
}

/** The glyphs the status line cycles through, one frame each. */
export const GLYPHS = ['✢', '✳', '✶', '✻', '✽'] as const;

/** How often the phrase changes, and how often the glyph turns. */
export const PHRASE_MS = 2_500;
export const GLYPH_MS = 150;
