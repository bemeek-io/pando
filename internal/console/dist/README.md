# Built console assets

`make console` writes the Vite build here. Everything in this directory except this file is ignored
by git, and the binary embeds the whole directory with `//go:embed all:dist`.

**This file is committed on purpose.** `go:embed` fails the build when its pattern matches nothing,
so without something here a fresh clone would not compile until somebody had run npm — a poor first
five minutes for a Go contributor with no interest in the console.

It also has to survive a build, which is why the emptying is done by `console/scripts/clean-dist.mjs`
rather than by Vite's `emptyOutDir`: that takes everything, and deleting this file put the repository
back to not compiling.

A binary built without running `make console` serves this file instead of the console and logs a line
saying so. The API, the proxy and the CLI are unaffected.
