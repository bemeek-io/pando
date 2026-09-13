package deploy

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/secret"
)

// specWithProvisionedSlot is an app whose DATABASE_URL comes from a service
// Pando stands up.
func specWithProvisionedSlot() *spec.AppSpec {
	key := "DATABASE_URL"
	return &spec.AppSpec{
		SchemaVersion: spec.SchemaVersion,
		AppID:         "app_01HQ8",
		Workloads: []spec.Workload{{
			Name: "web", Primary: true, Exposed: true,
			Env: []spec.EnvEntry{{Key: "DATABASE_URL", SlotRef: &key}},
		}},
		Slots: []spec.Slot{{
			Key: key, Type: spec.SlotPostgres, Required: true,
			Resolution: &spec.Resolution{Mode: spec.ResolutionProvisioned},
		}},
		Resources: spec.Resources{CPUMillis: 1000, MemoryBytes: 512 << 20},
		Retention: spec.Retention{LogBytes: 64 << 20},
	}
}

func provisionedPostgres() provisioned {
	return provisioned{
		workloads: []api.WorkloadPlan{{
			Name:   "svc-01hq9",
			Image:  "postgres:17-alpine",
			Mounts: []api.MountPlan{{VolumeID: "vol_01HQ9", Path: "/var/lib/postgresql/data"}},
		}},
		volumes:     []api.VolumePlan{{VolumeID: "vol_01HQ9", Name: "svc-01hq9-data"}},
		connections: map[string]secret.Value{"DATABASE_URL": secret.New("postgres://app:pw@svc-01hq9:5432/app")},
		names:       map[string]string{"DATABASE_URL": "svc-01hq9"},
	}
}

// TestR131_AProvisionedSlotReachesTheWorkloadAsAValue asserts R-131.
//
// The app never learns its database was provisioned rather than bound: it gets
// a connection string in an environment variable, which is the whole promise.
func TestR131_AProvisionedSlotReachesTheWorkloadAsAValue(t *testing.T) {
	s := specWithProvisionedSlot()
	plan, err := bundlePlanFor(s, "app:latest", nil, provisionedPostgres(), true)
	require.NoError(t, err)

	require.Equal(t, "postgres://app:pw@svc-01hq9:5432/app", plan.Workloads[0].Env["DATABASE_URL"].Reveal())
}

// Without the provisioning step the deploy stops rather than starting an app
// with a blank DATABASE_URL. An app that starts and cannot find its database is
// the failure mode R-132 exists to prevent, and it should not come back in by
// the side door.
func TestAnUnprovisionedSlotStopsTheDeploy(t *testing.T) {
	_, err := bundlePlanFor(specWithProvisionedSlot(), "app:latest", nil, provisioned{}, true)
	require.Error(t, err)
}

// TestR135_ProvisionedStorageIsAnOrdinaryAppVolume asserts R-135.
//
// The bundle carries it like any other volume, which is what makes it recorded,
// backed up, offered at delete and reclaimed afterwards by code that knows
// nothing about services.
func TestR135_ProvisionedStorageIsAnOrdinaryAppVolume(t *testing.T) {
	plan, err := bundlePlanFor(specWithProvisionedSlot(), "app:latest", nil, provisionedPostgres(), true)
	require.NoError(t, err)

	require.Len(t, plan.Volumes, 1)
	require.Equal(t, "vol_01HQ9", plan.Volumes[0].VolumeID)
}

// TestR222_AProvisionedServiceIsCappedLikeAnyOtherWorkload asserts R-222.
//
// A Postgres logging every connection onto a disk shared with twenty other apps
// is exactly the thing the cap exists for, and the adapter cannot know the
// app's limits.
func TestR222_AProvisionedServiceIsCappedLikeAnyOtherWorkload(t *testing.T) {
	s := specWithProvisionedSlot()
	plan, err := bundlePlanFor(s, "app:latest", nil, provisionedPostgres(), true)
	require.NoError(t, err)

	var service api.WorkloadPlan
	for _, w := range plan.Workloads {
		if w.Name == "svc-01hq9" {
			service = w
		}
	}
	require.NotEmpty(t, service.Name, "the service is in the bundle")
	require.Equal(t, s.Retention.LogBytes, service.LogBytes)
	require.Equal(t, s.Resources.MemoryBytes, service.Resources.MemoryBytes)
	require.Equal(t, s.Resources.CPUMillis, service.Resources.CPUMillis)
}

// The app workload starts after the service it reads a slot from.
//
// Start order, not readiness — Docker's depends_on does not wait for a health
// check. What covers the rest of the race is the restart policy.
func TestTheAppStartsAfterTheServiceItDependsOn(t *testing.T) {
	s := specWithProvisionedSlot()
	plan, err := bundlePlanFor(s, "app:latest", nil, provisionedPostgres(), true)
	require.NoError(t, err)

	require.Equal(t, []string{"svc-01hq9"}, plan.Workloads[0].DependsOn)
}

// A workload that reads no slot gains no dependency. Ordering every workload
// behind every service would serialise an app's startup for no reason.
func TestAWorkloadThatNeedsNoServiceWaitsForNothing(t *testing.T) {
	s := specWithProvisionedSlot()
	s.Workloads = append(s.Workloads, spec.Workload{Name: "worker"})

	plan, err := bundlePlanFor(s, "app:latest", nil, provisionedPostgres(), true)
	require.NoError(t, err)
	require.Empty(t, plan.Workloads[1].DependsOn)
}

// TestR148_AProvisionedServiceIsPartOfWhatShouldBeRunning asserts R-148 for a
// provisioned service.
//
// The reconciler's "want" is compared against what is running, every fifteen
// seconds. Leave the service out of it and two things go wrong at once: a
// killed database is never restored, because nothing wants it, and the running
// one is reported as a workload the spec does not declare — an app told
// repeatedly that its own database is a stranger.
func TestR148_AProvisionedServiceIsPartOfWhatShouldBeRunning(t *testing.T) {
	s := specWithProvisionedSlot()
	svcs := provisionedPostgres()

	plan, err := bundlePlanFor(s, "app:latest", nil, svcs, true)
	require.NoError(t, err)

	names := map[string]bool{}
	for _, w := range plan.Workloads {
		names[w.Name] = true
	}
	require.True(t, names["svc-01hq9"], "the service is in the bundle the reconciler compares against")
	require.True(t, names["web"])
}

// And the shape carries no environment, because building it must never be a
// reason to decrypt a secret (R-193). The comparison runs every fifteen
// seconds for every app; reading a connection string that often would make the
// cheap path the expensive one.
func TestTheComparedShapeCarriesNoEnvironment(t *testing.T) {
	plan, err := bundlePlanFor(specWithProvisionedSlot(), "app:latest", nil, provisionedPostgres(), false)
	require.NoError(t, err)

	for _, w := range plan.Workloads {
		require.Empty(t, w.Env, "%s carries environment into a shape comparison", w.Name)
	}
}
