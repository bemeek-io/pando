package reconciler

import (
	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/deploy"
	"github.com/bemeek-io/pando/internal/core/spec"
)

// PlanShape builds what should be running, without resolving any secret.
//
// The reconciler compares shape — which workloads, which images, which ports
// and mounts — and that comparison runs every fifteen seconds for every app. It
// must never be a reason to decrypt anything, so environment is deliberately
// absent here and drift in it is detected by fingerprint instead (R-193).
//
// Secrets are resolved only when there is something to apply, which is the
// uncommon case.
func PlanShape(s *spec.AppSpec, image string) api.BundlePlan {
	plan, err := deploy.BundlePlanShape(s, image)
	if err != nil {
		return api.BundlePlan{}
	}
	return plan
}

// EnvHash is the fingerprint of the environment an app should be running with.
func EnvHash(s *spec.AppSpec, versions map[string]int) string {
	return deploy.EnvFingerprint(s, versions)
}
