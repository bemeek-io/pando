// Clears the previous console build, but not the directory itself.
//
// Vite's own emptyOutDir would do this, and it takes everything — including
// dist/README.md, which is committed so that `//go:embed all:dist` has
// something to match on a fresh clone. With Vite doing the emptying, the first
// console build deleted that file and the next `git add -A` quietly staged its
// removal, putting the repository back to not compiling for anyone who had not
// run npm.
//
// So the emptying happens here instead, where it can be told what to leave.

import { readdirSync, rmSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const dist = resolve(dirname(fileURLToPath(import.meta.url)), '../../internal/console/dist');
const keep = new Set(['README.md']);

let entries;
try {
  entries = readdirSync(dist);
} catch {
  // Nothing built yet. Vite creates the directory.
  process.exit(0);
}

for (const entry of entries) {
  if (keep.has(entry)) continue;
  rmSync(join(dist, entry), { recursive: true, force: true });
}
