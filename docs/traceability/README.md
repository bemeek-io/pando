# Traceability

Requirements §-numbers move; **requirement IDs are stable**. Refer to `R-###`, never to a section
number.

`requirements-index.md` is generated. It answers three questions for all 207 requirements:

1. **Is it designed?** Which of `docs/design/00`–`08` reference it.
2. **Is it planned?** Which phase in `docs/plan/` builds it.
3. **Is it proven?** Which Go tests are named for it.

Regenerate with `make requirements-index` (or `python3 scripts/gen-requirements-index.py`). **Do not
hand-edit the output** — it will be overwritten.

## The test naming convention

From design 00 §4. Each testable requirement gets at least one acceptance test named for it:

```go
// TestR132_UnfilledRequiredSlotBlocksDeploy asserts R-132.
func TestR132_UnfilledRequiredSlotBlocksDeploy(t *testing.T) { … }
```

The generator scans every `*_test.go` for `TestR###`, so coverage is derived from the code rather than
tracked by hand. `make requirements-coverage` prints the same numbers without rewriting the file —
that is the form to run in CI.

## Reading the gaps

**A missing design reference is not automatically a gap.** Requirements fall into four categories, and
the index cannot tell them apart:

- **Philosophy** — R-002 ("setup cost is paid once") shapes every decision and is testable by none of
  them.
- **Non-goals** — R-010 through R-016 are things Pando refuses to do. The test is the absence of a
  feature.
- **Deferred** — R-290 and up are `[LATER]`. Designed for, deliberately not built.
- **Real gaps** — a requirement in scope for v1 that no design document addresses.

When you find one in the fourth category, say so explicitly rather than filing it under one of the
first three. The point of generating this index is to make that fourth category visible; quietly
reclassifying a gap defeats it.
