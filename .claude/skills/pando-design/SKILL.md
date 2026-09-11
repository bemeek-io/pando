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

## Before using a component

Each component has a `.prompt.md` beside it stating what it is for, when **not**
to use it, and the brand rule it enforces. Read that file before reaching for the
component. The rules are not stylistic preference — several encode product
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
| `PROVENANCE.md` | Where this came from and how to re-sync it. |

## Building a Pando interface

For production code, read the rules here and build with the tokens and
components. For a mockup or a throwaway prototype, copy `styles.css` and
`tokens/` next to a static HTML file and use the tokens directly — the visual
language holds without React.

If asked to design something the system does not cover, extend it the way the
existing additions were made: from the spec's own rules, and note the reasoning.
Do not introduce a new color, a new font or a new radius.
