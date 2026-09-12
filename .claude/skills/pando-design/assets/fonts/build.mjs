// Refreshes the self-hosted webfonts from Google Fonts.
//
// Downloads each .woff2 and rewrites tokens/fonts.css to point at local paths.
// It keeps Google's own CSS structure and swaps only the URLs: the
// unicode-ranges, the per-subset split and the weight declarations for the
// variable faces are theirs, and re-deriving them is how a subset quietly stops
// loading for someone whose app is named in Cyrillic.
//
// Filenames carry the weight because IBM Plex Mono ships a separate static file
// per weight while Newsreader and Public Sans are variable and share one file
// per subset. Naming by family and subset alone made the two Plex files collide
// and pointed the 400 face at the 500 glyphs.
//
//   node assets/fonts/build.mjs
import { writeFile } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));

// The exact families and axes the brand spec names, and nothing else.
const SOURCE =
  'https://fonts.googleapis.com/css2' +
  '?family=Newsreader:opsz,wght@6..72,400;6..72,500' +
  '&family=Public+Sans:wght@400;500;600' +
  '&family=IBM+Plex+Mono:wght@400;500' +
  '&display=swap';

// A browser user-agent, or Google serves the .ttf fallback instead of .woff2.
const UA =
  'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 ' +
  '(KHTML, like Gecko) Chrome/120.0 Safari/537.36';

const css = await (await fetch(SOURCE, { headers: { 'User-Agent': UA } })).text();
await writeFile(join(here, 'google.css'), css);

const blocks = [...css.matchAll(/\/\* ([a-z-]+) \*\/\s*@font-face \{(.*?)\}/gs)];

const names = new Map();
for (const [, subset, body] of blocks) {
  const family = /font-family:\s*'([^']+)'/.exec(body)[1];
  const weight = /font-weight:\s*([^;]+);/.exec(body)[1].trim().replace(/\s+/g, '-');
  const url = /url\((https:\/\/[^)]+)\)/.exec(body)[1];
  if (!names.has(url)) {
    names.set(url, `${family.toLowerCase().replace(/ /g, '-')}-${subset}-${weight}.woff2`);
  }
}

if (new Set(names.values()).size !== names.size) {
  throw new Error('font filenames collide — two files would overwrite each other');
}

let total = 0;
for (const [url, name] of names) {
  const bytes = new Uint8Array(await (await fetch(url)).arrayBuffer());
  await writeFile(join(here, name), bytes);
  total += bytes.length;
}

let local = css;
for (const [url, name] of names) {
  local = local.replaceAll(`url(${url})`, `url('../assets/fonts/${name}')`);
}

if (/https:\/\/fonts\.gstatic/.test(local)) {
  throw new Error('a remote font URL survived the rewrite');
}

console.log(`${names.size} files, ${Math.round(total / 1024)} KiB`);
console.log('now paste the rewritten @font-face rules into tokens/fonts.css,');
console.log('keeping its header comment.');
