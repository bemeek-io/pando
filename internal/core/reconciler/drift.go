package reconciler

import (
	"fmt"
	"sort"
	"strings"

	"github.com/bemeek-io/pando/internal/adapter/api"
)

// Drift is the difference between what a spec says and what is running.
type Drift struct {
	// Reconcilable are differences the reconciler may correct. Every one of
	// them is a thing to create or start.
	Reconcilable []Difference

	// ReportOnly are differences it must not. Every one of them would mean
	// destroying or overwriting something a person may have put there.
	ReportOnly []Difference
}

// Difference is one thing that does not match.
type Difference struct {
	Workload string
	Kind     string
	Detail   string
}

// None reports whether anything differs.
func (d Drift) None() bool { return len(d.Reconcilable) == 0 && len(d.ReportOnly) == 0 }

// Actionable reports whether there is anything the reconciler may fix.
func (d Drift) Actionable() bool { return len(d.Reconcilable) > 0 }

// Describe renders the drift for a notification or a log line.
func (d Drift) Describe() string {
	var parts []string
	for _, x := range append(append([]Difference{}, d.ReportOnly...), d.Reconcilable...) {
		if x.Workload != "" {
			parts = append(parts, fmt.Sprintf("%s: %s (%s)", x.Workload, x.Kind, x.Detail))
			continue
		}
		parts = append(parts, fmt.Sprintf("%s (%s)", x.Kind, x.Detail))
	}
	sort.Strings(parts)
	return strings.Join(parts, "; ")
}

// Kinds of difference.
const (
	DriftWorkloadMissing  = "workload missing"
	DriftWorkloadStopped  = "workload stopped"
	DriftWrongImage       = "wrong image"
	DriftStaleEnvironment = "stale environment"
	DriftVolumeMissing    = "volume missing"
	DriftVolumeLostData   = "volume that held data is missing"
	DriftUnknownWorkload  = "unrecognized workload"
	DriftRouteMissing     = "route missing"
)

// Classify compares what should be running against what is.
//
// The dividing line, and the rule to hold when new cases come up (design 05
// §2.1): **the reconciler may create and start things; it may not destroy
// anything a human may have wanted.**
//
// Everything in Reconcilable is a creation or a start. Everything in ReportOnly
// is something where the only way to "fix" it would be to throw away state or
// a container that someone put there deliberately — and being wrong about that
// is unrecoverable, while being wrong about reporting it costs a notification.
func Classify(want api.BundlePlan, observed api.ObservedBundle, in Inputs) Drift {
	var d Drift

	byName := map[string]api.ObservedWorkload{}
	for _, w := range observed.Workloads {
		byName[w.Name] = w
	}

	for _, w := range want.Workloads {
		found, exists := byName[w.Name]

		switch {
		case !exists || !found.Present:
			// Recreate. Nothing is being destroyed: it is already gone.
			d.Reconcilable = append(d.Reconcilable, Difference{
				Workload: w.Name, Kind: DriftWorkloadMissing,
				Detail: "the spec declares it and it is not running",
			})

		case !found.Running:
			d.Reconcilable = append(d.Reconcilable, Difference{
				Workload: w.Name, Kind: DriftWorkloadStopped,
				Detail: exitDetail(found),
			})

		case in.ExpectedDigest != "" && found.ImageDigest != "" && found.ImageDigest != in.ExpectedDigest:
			d.Reconcilable = append(d.Reconcilable, Difference{
				Workload: w.Name, Kind: DriftWrongImage,
				Detail: fmt.Sprintf("running %s, expected %s",
					shortDigest(found.ImageDigest), shortDigest(in.ExpectedDigest)),
			})
		}
	}

	// A workload inside the bundle that the spec does not mention. Someone put
	// it there; removing it is destructive and was not asked for (R-148).
	wanted := map[string]bool{}
	for _, w := range want.Workloads {
		wanted[w.Name] = true
	}
	for _, w := range observed.Workloads {
		if !wanted[w.Name] && w.Present {
			d.ReportOnly = append(d.ReportOnly, Difference{
				Workload: w.Name, Kind: DriftUnknownWorkload,
				Detail: "it is running inside this app's bundle and the spec does not declare it",
			})
		}
	}

	d.appendVolumeDrift(want, observed, in)

	// R-193: the one form of drift that cannot be observed, because
	// ObservedWorkload carries no environment and never will.
	if in.AppliedEnvHash != "" && in.CurrentEnvHash != "" && in.AppliedEnvHash != in.CurrentEnvHash {
		d.Reconcilable = append(d.Reconcilable, Difference{
			Kind:   DriftStaleEnvironment,
			Detail: "a value the app was started with has changed since",
		})
	}

	if in.RouteExpected && !in.RoutePresent {
		d.Reconcilable = append(d.Reconcilable, Difference{
			Kind: DriftRouteMissing, Detail: "traffic has no way to reach this app",
		})
	}

	return d
}

// appendVolumeDrift splits missing volumes by whether they ever held data.
//
// This is the case R-203 exists for, and the most important line in the file.
// Recreating a volume that previously held data produces an empty volume and an
// app that comes up healthy having lost everything — the failure that looks
// exactly like success, and the reason persistence earns a warning elsewhere
// even though inference is otherwise forbidden. A volume that has never been
// attached to anything cannot lose data by being created, so that one is safe.
func (d *Drift) appendVolumeDrift(want api.BundlePlan, observed api.ObservedBundle, in Inputs) {
	present := map[string]bool{}
	for _, v := range observed.Volumes {
		if v.Present {
			present[v.VolumeID] = true
		}
	}

	for _, v := range want.Volumes {
		if present[v.VolumeID] {
			continue
		}
		if in.VolumesThatHeldData[v.VolumeID] {
			d.ReportOnly = append(d.ReportOnly, Difference{
				Kind:   DriftVolumeLostData,
				Detail: v.Name + " is gone, and Pando will not recreate it: an empty replacement would look healthy while the data it held is lost",
			})
			continue
		}
		d.Reconcilable = append(d.Reconcilable, Difference{
			Kind: DriftVolumeMissing, Detail: v.Name + " has never held data, so creating it loses nothing",
		})
	}
}

// Inputs are the facts Classify needs that are not in the bundle plan.
type Inputs struct {
	// ExpectedDigest is the image the last successful deployment ran. Empty
	// when unknown, which makes image drift undetectable rather than making
	// every workload look wrong.
	ExpectedDigest string

	// AppliedEnvHash and CurrentEnvHash stand in for environment observation
	// (R-193). Either being empty means the comparison cannot be made, and an
	// unanswerable question is not drift.
	AppliedEnvHash string
	CurrentEnvHash string

	// VolumesThatHeldData is the difference between a volume Pando may recreate
	// and one it must only report.
	VolumesThatHeldData map[string]bool

	RouteExpected bool
	RoutePresent  bool
}

func exitDetail(w api.ObservedWorkload) string {
	if w.ExitCode != nil {
		return fmt.Sprintf("it exited with status %d", *w.ExitCode)
	}
	return "it exists but is not running"
}

func shortDigest(d string) string {
	d = strings.TrimPrefix(d, "sha256:")
	if len(d) > 12 {
		return d[:12]
	}
	return d
}
