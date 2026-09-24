// Brand adherence, as ESLint flat config.
//
// `_adherence.oxlintrc.json` holds the rules: a raw hex color, a raw px value, a
// font that is not Newsreader / Public Sans / IBM Plex Mono, and an import that
// reaches past the barrel. **They do not run under oxlint**, whatever the file
// is called. Both rules it uses are unimplemented there: `no-restricted-syntax`
// is absent from `oxlint --rules` entirely, and `no-restricted-imports` has no
// checkmark. Older oxlint drops the rule without a word and passes a hex value
// clean; newer oxlint refuses to load the file. Its contents are an ESLint
// config — `no-restricted-syntax` with esquery selectors is an ESLint core rule.
//
// So this reads the JSON and hands the rules to ESLint, which implements both.
// Read rather than restated: the design project is the source of truth for the
// visual language (PROVENANCE.md), and a second copy of the selectors would be
// a second opinion about the brand.
//
// Use it from a consumer's eslint.config.mjs, pointing at the vendored copy:
//
//   import parser from '@typescript-eslint/parser';
//   import react from 'eslint-plugin-react';
//   import { adherence } from './src/design/adherence.eslint.mjs';
//
//   export default adherence({ parser, react, files: ['src/**/*.{ts,tsx}'], ignores: ['src/design/**'] });
//
// The parser and the React plugin are passed in rather than imported here, so
// this file resolves nothing from wherever it happens to be copied to.

import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const rules = JSON.parse(readFileSync(resolve(here, '_adherence.oxlintrc.json'), 'utf8')).rules;

/** Re-severity a rule entry from warn to error, keeping its options. Upstream ships every rule as "warn", and a warning fails nothing. */
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
 * `onClick` on a button, and they reject the design project's own console
 * prototype, which is written as `<Button variant="primary" onClick={onAdd}>`.
 *
 * TypeScript checks the same thing correctly, from the same 24 `.d.ts` files,
 * including the inherited attributes. That is the check kept.
 */
function brandSelectorsOnly(entry) {
  if (!Array.isArray(entry)) return 'off';
  const selectors = entry.slice(1).filter((rule) => brandRules.test(rule.message ?? ''));
  return selectors.length > 0 ? ['error', ...selectors] : 'off';
}

/**
 * The adherence rules as a flat config array, at error severity.
 *
 * @param {{ parser: object, react: object, files: string[], ignores?: string[] }} options
 *   `parser` is `@typescript-eslint/parser`, `react` is `eslint-plugin-react`.
 *   `ignores` should include the vendored design system itself, which is
 *   upstream's to lint, not the consumer's.
 */
export function adherence({ parser, react, files, ignores = [] }) {
  return [
    {
      files,
      ignores,
      languageOptions: {
        parser,
        parserOptions: { ecmaVersion: 'latest', sourceType: 'module', ecmaFeatures: { jsx: true } },
      },
      plugins: { react },
      rules: {
        'no-restricted-syntax': brandSelectorsOnly(rules['no-restricted-syntax']),
        'no-restricted-imports': asError(rules['no-restricted-imports']),
      },
    },
  ];
}
