//go:build integration

package reconciler_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/trypando/pando/internal/core/policy"
	"github.com/trypando/pando/internal/core/reconciler"
	"github.com/trypando/pando/internal/core/spec"
	"github.com/trypando/pando/internal/core/state"
	"github.com/trypando/pando/internal/id"
)

type movedHead struct{ sha string }

func (m movedHead) Resolve(context.Context, spec.Source) (string, error) { return m.sha, nil }

type mutablePolicy struct {
	mu  sync.Mutex
	doc policy.Document
}

func (p *mutablePolicy) Load(context.Context) (policy.Document, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.doc, nil
}

func (p *mutablePolicy) set(doc policy.Document) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.doc = doc
}

// TestR158_AutoDeploySkipsAnAppThatNeedsApproval asserts that an app which
// already auto-deploys stops doing so when policy starts requiring approval
// for it — skipped, not queued as a request per push — and starts again when
// policy stops.
func TestR158_AutoDeploySkipsAnAppThatNeedsApproval(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := connected(t)
	apps := state.NewApps(db)
	deployments := state.NewDeployments(db)
	owner := seedOwner(t, db)

	app, err := apps.Create(ctx, "auto-"+id.New(id.App), id.New(id.App), owner, owner,
		spec.Source{Type: spec.SourceGit, URL: "https://example.test/app", Ref: "main"})
	require.NoError(t, err)
	rev, err := apps.CreateRevision(ctx, app.ID, &spec.AppSpec{
		SchemaVersion: spec.SchemaVersion,
		AppID:         app.ID,
		Source:        spec.Source{Type: spec.SourceGit, URL: "https://example.test/app", Ref: "main", Commit: "aaaaaaa"},
		Build:         spec.Build{Strategy: spec.BuildPrebuilt},
		Workloads:     []spec.Workload{{Name: "web", Image: "example/app:1", Primary: true, Exposed: true}},
		Routing:       spec.Routing{AdapterRef: "rte_fake", Mode: spec.RoutingPort, Port: 9000},
		Runtime:       spec.RuntimeRef{AdapterRef: "rt_fake", IsolationFloor: spec.IsolationContainer},
		Deploy: spec.Deploy{Strategy: spec.DeployRecreate,
			AutoDeploy: spec.AutoDeploy{Enabled: true, Trigger: spec.TriggerBranchUpdated, Branch: "main"}},
	}, spec.OriginEdited, owner)
	require.NoError(t, err)
	require.NoError(t, apps.Pin(ctx, app.ID, rev.ID, state.StateRunning, owner))

	pol := &mutablePolicy{doc: policy.Document{DeployApprovalApps: []string{app.ID}}}
	core, logs := observer.New(zap.InfoLevel)
	var enqueued []string
	job := &reconciler.AutoDeploy{
		Apps:        apps,
		Deployments: deployments,
		Resolver:    movedHead{sha: "bbbbbbb"},
		Enqueue: func(_ context.Context, dep state.Deployment, _ state.Revision) {
			enqueued = append(enqueued, dep.ID)
		},
		Logger: zap.New(core),
		Policy: pol,
	}

	job.Poll(ctx)
	deps, err := deployments.ListForApp(ctx, app.ID)
	require.NoError(t, err)
	require.Empty(t, deps, "no deploy, and no request waiting for approval either")
	require.Empty(t, enqueued)
	require.Equal(t, 1, logs.FilterMessage("auto-deploy skipped: this app's deploys need approval").Len())

	// Policy stops requiring it, and the next poll deploys the new commit.
	pol.set(policy.Document{})
	job.Poll(ctx)
	deps, err = deployments.ListForApp(ctx, app.ID)
	require.NoError(t, err)
	require.Len(t, deps, 1)
	require.Equal(t, state.DeployPending, deps[0].Status)
	require.Len(t, enqueued, 1)
}
