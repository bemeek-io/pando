package planner_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/core/planner"
	"github.com/bemeek-io/pando/internal/core/policy"
	"github.com/bemeek-io/pando/internal/errs"
)

// Log retention (R-222–R-224), which is O-16's resolution.
//
// The decision: cap per app at creation, and enforce the aggregate as a
// plan-time bound on the **sum of committed caps** rather than as an
// observation of usage. Bounding what is promised is stronger than watching
// what accumulates — and it is the only option that does not require recreating
// containers, which the reconciler may not do because an unrelated app turned
// chatty.

// TestR224_TheAggregateLogBudgetIsEnforcedAtPlanTime asserts the give-up point
// is before the deploy, not after the disk fills.
func TestR224_TheAggregateLogBudgetIsEnforcedAtPlanTime(t *testing.T) {
	ctx := context.Background()

	doc := policy.Default()
	doc.MaxLogDiskBytes = 500 << 20 // 500 MB across the install

	rt := capableRuntime()
	p := planner.New(
		registry(t, rt, capableRouting(), capableBuilder()),
		policy.Static(doc),
		fixedAllocations{alloc: planner.Allocation{LogBytes: 400 << 20}},
	)

	// 400 MB already committed by other apps, and this one wants 200.
	appSpec := plannableSpec()
	appSpec.Retention.LogBytes = 200 << 20

	_, err := p.Check(ctx, appSpec)
	require.Error(t, err)
	require.Equal(t, errs.CapacityWouldOversubscribe, errs.CodeOf(err))

	// The message has to be actionable: what it would commit, and what is
	// allowed. "Capacity exceeded" tells an operator nothing they can do.
	require.Contains(t, err.Error(), "600")
	require.Contains(t, err.Error(), "500")

	// Under the budget, the same app plans.
	appSpec.Retention.LogBytes = 50 << 20
	_, err = p.Check(ctx, appSpec)
	require.NoError(t, err)
}

// TestR224_NoBudgetMeansNoAggregateCheck — an install that has not set a limit
// is not second-guessed. Pando ships permissive (R-270).
func TestR224_NoBudgetMeansNoAggregateCheck(t *testing.T) {
	ctx := context.Background()

	p := planner.New(
		registry(t, capableRuntime(), capableRouting(), capableBuilder()),
		policy.Static(policy.Default()),
		// Absurd, and irrelevant: with no budget set there is nothing to
		// compare it against.
		fixedAllocations{alloc: planner.Allocation{LogBytes: 100 << 30}},
	)

	appSpec := plannableSpec()
	appSpec.Retention.LogBytes = 100 << 20

	_, err := p.Check(ctx, appSpec)
	require.NoError(t, err)
}

// TestR222_ARuntimeThatCannotCapLogsIsRefusedWhenABudgetExists asserts the
// capability half.
//
// A runtime that cannot bound logs cannot honor an aggregate budget, so an
// install that has set one refuses the deploy with the reason — rather than
// accepting it and leaving the logs quietly unbounded, which is the outcome
// R-224 exists to prevent.
func TestR222_ARuntimeThatCannotCapLogsIsRefusedWhenABudgetExists(t *testing.T) {
	ctx := context.Background()

	doc := policy.Default()
	doc.MaxLogDiskBytes = 1 << 30

	rt := capableRuntime()
	rt.caps.LogRetention.SupportsSizeCap = false
	p := planner.New(
		registry(t, rt, capableRouting(), capableBuilder()),
		policy.Static(doc),
		fixedAllocations{},
	)

	appSpec := plannableSpec()
	appSpec.Retention.LogBytes = 100 << 20

	_, err := p.Check(ctx, appSpec)
	require.Error(t, err)
	require.Equal(t, errs.PlanCapabilityUnsupported, errs.CodeOf(err))
	require.Contains(t, err.Error(), "cannot limit")
}
