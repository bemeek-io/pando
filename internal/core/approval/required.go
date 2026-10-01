// Package approval is deploy approval (R-154 – R-159): whether a deploy needs
// somebody's sign-off, and the request, approval, rejection and expiry of one.
package approval

import (
	"github.com/trypando/pando/internal/core/policy"
	"github.com/trypando/pando/internal/core/spec"
)

// Reason is why a deploy needs approval. More than one can apply, and the
// console and the audit log name each.
type Reason string

const (
	// ReasonInstall: host policy requires approval for every app.
	ReasonInstall Reason = "install"
	// ReasonAppPolicy: host policy requires it for this app, and its owner
	// cannot turn that off.
	ReasonAppPolicy Reason = "app_policy"
	// ReasonAppSpec: the app requires it of itself, in the running spec or
	// the one being deployed.
	ReasonAppSpec Reason = "app_spec"
	// ReasonEgressLoosening: the deploy loosens the install's egress rules
	// and policy says loosening needs approval (R-183).
	ReasonEgressLoosening Reason = "egress_loosening"
)

// Message says what a reason means, for the person being asked to approve.
func (r Reason) Message() string {
	switch r {
	case ReasonInstall:
		return "This installation requires approval for every deploy."
	case ReasonAppPolicy:
		return "This installation requires approval for this app's deploys."
	case ReasonAppSpec:
		return "This app requires approval for its own deploys."
	case ReasonEgressLoosening:
		return "This deploy loosens the installation's egress rules, which needs approval."
	}
	return string(r)
}

// Reasons is why deploying next over running needs approval, or nothing when
// it does not (R-154). running is the app's pinned spec, nil for an app that
// has never had one.
//
// The app's own requirement is read from both specs, so that turning it off
// is itself a deploy that needs approval. An egress loosening counts only
// when it is new: one the running spec already carries was approved when it
// was deployed, and a later push under it is not asking for anything more.
func Reasons(doc policy.Document, appID string, running, next *spec.AppSpec) []Reason {
	var out []Reason
	if doc.DeployApprovalRequired {
		out = append(out, ReasonInstall)
	}
	for _, id := range doc.DeployApprovalApps {
		if id == appID {
			out = append(out, ReasonAppPolicy)
			break
		}
	}
	if (running != nil && running.Deploy.RequireApproval) || (next != nil && next.Deploy.RequireApproval) {
		out = append(out, ReasonAppSpec)
	}
	if next != nil && doc.EgressLooseningRule() == policy.EgressLooseningApproval {
		var before policy.EffectiveEgress
		if running != nil {
			before = doc.EgressFor(running.Egress)
		}
		if len(policy.NewLoosenings(before, doc.EgressFor(next.Egress))) > 0 {
			out = append(out, ReasonEgressLoosening)
		}
	}
	return out
}

// BlocksAutoDeploy reports whether an app's deploys need approval whatever
// they change, which is what refuses auto-deploy (R-158). An egress loosening
// is not one of these reasons: it needs approval only for the deploy that
// introduces it, and an automatic deploy changes the commit and nothing else.
func BlocksAutoDeploy(doc policy.Document, appID string, s *spec.AppSpec) bool {
	for _, r := range Reasons(doc, appID, s, s) {
		if r != ReasonEgressLoosening {
			return true
		}
	}
	return false
}
