// Brand adherence, enforced.
//
// A raw hex color, a raw px value, a font that is not one of the three, or an
// import that reaches past the design system's barrel fails the build, the way
// the adapter import rule does for R-027. The rules come from the design
// system's `_adherence.oxlintrc.json`, run under ESLint because oxlint
// implements neither rule the file uses; `adherence.eslint.mjs` beside it
// explains, and is the one loader every consumer of the design system shares.
//
// Component props are checked by TypeScript against the design system's 24
// declaration files, not here.

import parser from '@typescript-eslint/parser';
import react from 'eslint-plugin-react';

import { adherence } from './src/design/adherence.eslint.mjs';

export default adherence({
  parser,
  react,
  files: ['src/**/*.{ts,tsx}'],
  // The design system itself, and generated files, are not the console's to
  // lint: one is upstream's, the other is the Go types' shape.
  ignores: ['src/design/**', 'src/api/types.gen.ts'],
});
