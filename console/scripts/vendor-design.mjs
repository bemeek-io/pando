// Copies the design system into the console's build tree.
//
// The system lives in .claude/skills/pando-design/, which PROVENANCE.md calls
// the imported copy of the design project — itself the source of truth. Keeping
// a second copy under version control here would be a third place for the visual
// language to live, and the one most likely to be edited by someone in a hurry.
//
// So this copies at build time and the destination is gitignored. One copy in
// git, one source of truth upstream, and a build that fails loudly if the skill
// directory is missing rather than quietly producing an unstyled console.
import { cp, rm, access } from 'node:fs/promises';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const source = resolve(here, '../../.claude/skills/pando-design');
const target = resolve(here, '../src/design');

try {
  await access(source);
} catch {
  console.error(
    `The design system is not at ${source}.\n` +
      'The console is built from it and cannot be built without it. See\n' +
      '.claude/skills/pando-design/PROVENANCE.md for how to restore it.',
  );
  process.exit(1);
}

await rm(target, { recursive: true, force: true });
await cp(source, target, { recursive: true });
console.log(`design system vendored from ${source}`);
