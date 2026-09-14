#!/usr/bin/env python3
"""Fail if govulncheck found a vulnerability that is not on the allowlist.

govulncheck itself has no suppression mechanism: it exits non-zero if anything is
reachable, and there is no way to say "this one, and only this one, is
understood". Without that, a module-level advisory with no fixed version makes
the check red forever, and a check that is always red stops being read.

So the check runs to completion, and this decides. A finding whose OSV ID is in
.github/govulncheck-allowlist.txt passes; anything else fails and is printed.
The allowlist requires a written reason per entry, which is the part that keeps
this from becoming a way to silence the scanner.

Usage:
    govulncheck -format=json ./... > vulns.json
    scripts/check-vulns.py vulns.json .github/govulncheck-allowlist.txt
"""

from __future__ import annotations

import json
import sys
from pathlib import Path


def findings(raw: str) -> tuple[dict[str, str], set[str]]:
    """Return (osv id -> summary) and the set of IDs actually called.

    The JSON output is a stream of concatenated objects rather than one array,
    so it is decoded incrementally. Each object carries exactly one of "osv",
    "finding", "config" or "progress".

    A finding is *called* when the first entry of its trace names a function.
    Findings without one are vulnerabilities in a module that is required or
    imported but whose vulnerable code is never reached, which govulncheck
    reports for information and does not exit non-zero over.
    """
    summaries: dict[str, str] = {}
    called: set[str] = set()

    decoder = json.JSONDecoder()
    index = 0
    while index < len(raw):
        if raw[index].isspace():
            index += 1
            continue
        message, end = decoder.raw_decode(raw, index)
        index = end

        if osv := message.get("osv"):
            summaries[osv["id"]] = osv.get("summary", "").strip()
        elif finding := message.get("finding"):
            trace = finding.get("trace") or []
            if trace and trace[0].get("function"):
                called.add(finding["osv"])

    return summaries, called


def allowed(path: Path) -> set[str]:
    ids = set()
    for line in path.read_text().splitlines():
        line = line.split("#", 1)[0].strip()
        if line:
            ids.add(line)
    return ids


def main() -> int:
    if len(sys.argv) != 3:
        print(__doc__, file=sys.stderr)
        return 2

    results, allowlist = Path(sys.argv[1]), Path(sys.argv[2])
    summaries, called = findings(results.read_text())
    accepted = allowed(allowlist)

    unexpected = sorted(called - accepted)
    stale = sorted(accepted - called)

    for osv in unexpected:
        print(f"::error::{osv}: {summaries.get(osv, 'no summary')}")
        print(f"  https://pkg.go.dev/vuln/{osv}")

    # A stale entry is not a failure — the run that clears it is usually the
    # upgrade that fixed it, and failing there would block the fix. It is worth
    # saying, because an allowlist nobody prunes is how the next real finding
    # gets hidden.
    for osv in stale:
        print(f"::notice::{osv} is on the allowlist but was not reported. "
              f"Remove it from {allowlist} if it has been fixed.")

    if unexpected:
        print(f"\n{len(unexpected)} vulnerability(ies) not on the allowlist. "
              f"Fix them, or add an entry to {allowlist} saying why not.")
        return 1

    if called:
        print(f"No new vulnerabilities. {len(called)} allowlisted: {', '.join(sorted(called))}.")
    else:
        print("No vulnerabilities reported.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
