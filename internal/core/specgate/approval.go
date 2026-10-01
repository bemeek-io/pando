package specgate

import (
	"github.com/trypando/pando/internal/core/approval"
	"github.com/trypando/pando/internal/core/policy"
	"github.com/trypando/pando/internal/core/spec"
)

// ApprovalReason is one reason a deploy needs approval, with what it means
// for the person reading the plan.
type ApprovalReason struct {
	Reason  approval.Reason `json:"reason"`
	Message string          `json:"message"`
}

// Approval is whether deploying a spec would need approval, and why (R-154).
// What the plan shows before anybody asks for the deploy.
type Approval struct {
	Required bool             `json:"required"`
	Reasons  []ApprovalReason `json:"reasons"`
}

// ApprovalFor is whether deploying next over running needs approval. running
// is the spec the app runs now, nil for an app that has never deployed.
func ApprovalFor(doc policy.Document, appID string, running, next *spec.AppSpec) Approval {
	out := Approval{Reasons: []ApprovalReason{}}
	for _, r := range approval.Reasons(doc, appID, running, next) {
		out.Reasons = append(out.Reasons, ApprovalReason{Reason: r, Message: r.Message()})
	}
	out.Required = len(out.Reasons) > 0
	return out
}
