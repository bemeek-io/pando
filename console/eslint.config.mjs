// Brand adherence, enforced.
//
// The design system ships `_adherence.oxlintrc.json`: rules that catch a raw hex
// colour, a raw px value, a font that is not one of the three, an import that
// reaches past the barrel, and a wrong prop on any of the 24 components. The
// phase asks for those to fail the build "the way the adapter import rule does
// for R-027".
//
// **They do not run under oxlint.** Both rules the file uses are unimplemented
// there: `no-restricted-syntax` is absent from `oxlint --rules` entirely, and
// `no-restricted-imports` is listed without a checkmark. A hex value and a px
// value both linted clean, and `--print-config` showed `no-restricted-syntax`
// dropped from the resolved config without a word. The file's *contents* are an
// ESLint config — `no-restricted-syntax` with esquery selectors is an ESLint
// core rule — so only its name says otherwise.
//
// So the rules are read from the vendored JSON and handed to ESLint, which
// implements both. Read rather than restated: the design project is the source
// of truth for the visual language (PROVENANCE.md), and a second copy of 40
// selectors here would be a second opinion about the brand.
//
// Severity is raised to error. Upstream ships every rule as "warn" and a
// warning does not fail anything.

import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import parser from '@typescript-eslint/parser';
import react from 'eslint-plugin-react';

const here = dirname(fileURLToPath(import.meta.url));
const adherencePath = resolve(here, 'src/design/_adherence.oxlintrc.json');

const adherence = JSON.parse(readFileSync(adherencePath, 'utf8'));

/** Re-severity a rule entry from warn to error, keeping its options. */
function asError(entry) {
  if (!Array.isArray(entry)) return 'error';
  return ['error', ...entry.slice(1)];
}

// The brand rules: raw hex, raw px, non-brand font. These are the ones worth
// running, and the ones nothing else catches.
const brandRules = /Raw hex|Raw px|Font not provided/;

/**
 * Keep the brand selectors and drop the per-component prop selectors.
 *
 * The prop selectors are **incorrect**, not merely redundant. Each enumerates
 * only its interface's own members — `<Button>`'s list is variant, size, icon,
 * disabled, fullWidth, children — while every component extends an HTML
 * attributes interface: `ButtonProps extends
 * Omit<React.ButtonHTMLAttributes<HTMLButtonElement>, 'size'>`. So they reject
 * `onClick` on a button. They reject the design project's own console
 * prototype, which is written as `<Button variant="primary" onClick={onAdd}>`.
 *
 * TypeScript checks the same thing correctly, from the same 24 `.d.ts` files,
 * including the inherited attributes — `variant="nonsense"` is caught, and
 * `onClick` is not. That is the check the phase asked the declaration files to
 * provide, so it is the one kept.
 *
 * Reported upstream rather than corrected here: PROVENANCE.md makes the design
 * project the source of truth and says a change belongs there first.
 */
function brandSelectorsOnly(entry) {
  if (!Array.isArray(entry)) return 'off';
  const selectors = entry.slice(1).filter((rule) => brandRules.test(rule.message ?? ''));
  return selectors.length > 0 ? ['error', ...selectors] : 'off';
}

export default [
  {
    files: ['src/**/*.{ts,tsx}'],

    // The design system itself, and generated files, are not the console's to
    // lint: one is upstream's, the other is the Go types' shape.
    ignores: ['src/design/**', 'src/api/types.gen.ts'],

    languageOptions: {
      parser,
      parserOptions: {
        ecmaVersion: 'latest',
        sourceType: 'module',
        ecmaFeatures: { jsx: true },
      },
    },

    plugins: { react },

    rules: {
      'no-restricted-syntax': brandSelectorsOnly(adherence.rules['no-restricted-syntax']),
      'no-restricted-imports': asError(adherence.rules['no-restricted-imports']),
    },
  },
];
