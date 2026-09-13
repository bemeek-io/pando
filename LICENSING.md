# Licensing

Pando is free and open source under the **GNU Affero General Public License, version 3**, and is also
available under a **commercial licence** for the cases AGPL does not suit.

Most people need only the first. This page exists so you can tell which one you are, without reading
a licence.

## If you are running Pando

**Use it. Nothing is asked of you.**

Hosting your own apps on your own machine — for yourself, your team, your company, your customers —
is covered by the AGPL with no obligation and no fee. Modify it, run the modified version, keep the
changes to yourself. The AGPL's one distinctive condition (§13) is about offering *Pando itself* to
other people over a network, not about the apps you host with it.

Running your company's internal tools behind Pando does not make your company's tools AGPL. Pando is
the thing under the licence; what it hosts is yours.

## If you are building on Pando

**Contributions are welcome, and a pull request is the way in.** Adapters are compiled in-tree — there
is no external plugin protocol and none is planned (R-253) — so a new runtime, routing, builder,
secrets, services, identity or notification provider is a contribution rather than a package.

Because Pando is dual-licensed, a contribution has to be available under both licences. **By opening a
pull request you agree that your contribution may be distributed under the AGPL and under the
commercial licence.** You keep the copyright in what you wrote; you are granting permission, not
signing it away. There is no CLA to sign and no copyright assignment.

If that does not work for you, say so in the pull request before it is merged — it is easier to
discuss than to unpick.

## If you are redistributing or reselling Pando

This is where the AGPL asks something of you, and where the commercial licence exists.

Under the **AGPL**, if you distribute Pando or run a modified version as a service other people use,
those people must be able to get the corresponding source of the version you are running — including
your modifications. That is §13, the clause that separates AGPL from GPL, and it applies to Pando's
own source, not to the applications Pando hosts for you.

A **commercial licence** removes that condition. It is intended for:

- **Offering Pando as a hosted service** — your own deployment platform built on it — without
  publishing your modifications.
- **Embedding Pando in a proprietary product** you ship to customers.
- **Distributing Pando inside an appliance or image** under terms of your own.
- An organisation whose policy or contracts make copyleft unworkable, independent of what is actually
  being built.

Terms are negotiated per case. Open an issue saying what you want to do, or contact
[bemeek-io](https://github.com/bemeek-io).

## Which licence applies to a given copy

The AGPL, unless you have a signed commercial agreement that says otherwise. Nothing in this
repository is under the commercial licence by default, and no file carries a different licence from
the rest.

## Why this way round

**AGPL → MIT is reversible; MIT → AGPL is not.** A project that starts permissive has already given
away the choice, because every existing copy stays permissive forever. Starting at AGPL keeps both
doors open: it can be relaxed later if that turns out to be right, and until then contributions are
not quietly absorbed into somebody's closed fork.

This is not a bet against commercial use — it is the reason the commercial licence exists rather than
being an afterthought. Nothing in the codebase assumes either answer.

## Third-party components

Pando's dependencies keep their own licences, and the AGPL does not extend to them. Two worth naming
because their files ship inside this repository rather than being fetched:

- **Webfonts** — Newsreader, Public Sans and IBM Plex Mono, in
  `.claude/skills/pando-design/assets/fonts/`, each under the SIL Open Font License 1.1. The licence
  and its copyright notices travel with them, in
  [`OFL.txt`](.claude/skills/pando-design/assets/fonts/OFL.txt) in that directory.
- **Go and npm dependencies** — see `go.mod` and `console/package.json`.

## Full text

[`LICENSE`](LICENSE) is the unmodified AGPL-3.0. Where this page and the licence disagree, the licence
is what holds — this is a summary written to be read, not a legal instrument, and it is not legal
advice.
