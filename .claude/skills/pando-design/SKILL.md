---
name: pando-design
description: The Pando design system — "Topo map". Use when building or changing any Pando user interface: the console, the marketing site, docs, mockups, or a throwaway prototype. Contains the design tokens, React components, brand rules, voice guidance and an adherence lint config. Invoke before writing UI code or CSS so that colors, type, spacing and copy come from the system rather than being invented.
---

# Pando design system — "Topo map"

Read `readme.md` first. It is the ground truth: principles, color with measured
contrast, typography, layout, shape, the contour illustration system, the logo,
components, icons, motion, copy and the avoid-list.

## Use it, don't reinvent it

Consumers link one file:

```html
<link rel="stylesheet" href="styles.css">
```

Dark theme is one attribute on the root: `<html data-theme="dark">`.

Everything visual comes from a CSS custom property. **A raw hex color, a raw `px`
value or a font family that is not Newsreader / Public Sans / IBM Plex Mono is a
mistake**, and `_adherence.oxlintrc.json` is configured to flag each one. Run it
against UI code before calling that code done.

Import components from `index.js`, not from a component's own file — the same
adherence config enforces that, so that a later refactor of a component's
internals cannot break its consumers.

## Before using a component: fetch its `.prompt.md`

Every component has a `.prompt.md` stating what it is for, when **not** to use
it, and the brand rule it enforces — and a `.d.ts` giving its exact props.

**Those two files are not in this directory.** Only `Button`'s are, kept as a
worked example of the shape. The rest live in the design project and are fetched
on demand, so that this skill stays small and cannot drift from upstream.

Fetch one before reaching for a component you have not used before:

```
DesignSync(method: "get_file",
           projectId: "27583278-89f3-4207-b9af-3fa14d54764a",
           path: "components/core/Card.prompt.md")
```

The path is always `components/<group>/<Name>.prompt.md`, with `<group>` one of
`brand`, `code`, `core`, `data`, `feedback`, `forms`, `navigation` — the same
layout as this directory, so a local component file tells you its remote path.

**If `DesignSync` reports it needs authorization, stop and ask the person to run
`/design-login`.** It is a command they type; you cannot run it, and there is no
way around it. Say so plainly rather than guessing at the component's props.

Pull the full set — all 24 `.d.ts` and all 24 `.prompt.md` — when starting phase 8
in earnest. The console is TypeScript, so without the `.d.ts` every component
types as `any`, which silently removes the prop checking the adherence config
exists to enforce.

The rules in those files are not stylistic preference — several encode product
decisions, for example:

- Status is **always a symbol plus a word**, never a colored pill, so it never
  depends on color alone.
- One `primary` button per view.
- Radius signals hierarchy: 2px tags, 4px buttons and inputs, 6px bounded
  objects, 8px dialogs. Never one radius for everything.
- Content **never** casts a shadow. Only popovers, toasts and dialogs do.
- The contour map is the one bold element and appears in five defined places. If
  a screen already has one, it gets no other decoration.

## Voice

Plain, literal, unhurried. Sentence case everywhere, including buttons and table
headers. Second person for the user; Pando refers to itself by name, never "we".
No emoji, no exclamation marks, no hype words.

**Errors say what happened and what to do**, with no apology and no `Error:`
prefix:

> Pando couldn't find a start command. Add one in app settings.

That is the same standard the API's error envelope is held to (R-105 in
`docs/requirements.md`) — an error a person can act on, or paste into the
assistant that wrote their app. The two should read as one product.

**An action keeps its name through the whole flow.** "Deploy" produces
"Deployed": the button, the toast and the log line use the same word.

## What is here

| Path | What's there |
| --- | --- |
| `styles.css` | The entry point. `@import` lines only. |
| `tokens/` | Colors (light + dark), type, spacing, radius, elevation, motion, base. |
| `components/` | 24 React components, grouped by concern. No npm dependencies, no CSS-in-JS. |
| `index.js` | The barrel every consumer imports from. |
| `readme.md` | The brand spec in full. Read this first. |
| `_adherence.oxlintrc.json` | Lint rules that catch raw hex, raw px, wrong fonts and wrong props. |
| `PROVENANCE.md` | Where this came from, what was left behind, and how to fetch it. |

Not here, fetched on demand — see `PROVENANCE.md` for each path:

| What | When you want it |
| --- | --- |
| `<Name>.prompt.md` | Before using a component for the first time. |
| `<Name>.d.ts` | Phase 8, all 24 — the console is TypeScript. |
| `ui_kits/console/` | Before designing a console screen. It is a working prototype of the apps table, app detail, build log, variables, settings and add-app flow, built from the brand spec's own wireframes. |
| `ui_kits/site/` | If building the marketing site or docs. |
| `guidelines/*.html` | Visual reference for color, type, spacing and brand. The prose rules are already in `readme.md`. |

## Building a Pando interface

For production code, read the rules here and build with the tokens and
components. For a mockup or a throwaway prototype, copy `styles.css` and
`tokens/` next to a static HTML file and use the tokens directly — the visual
language holds without React.

If asked to design something the system does not cover, extend it the way the
existing additions were made: from the spec's own rules, and note the reasoning.
Do not introduce a new color, a new font or a new radius.
