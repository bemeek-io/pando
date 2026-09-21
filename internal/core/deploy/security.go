package deploy

import (
	"context"
	"fmt"
	"io"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/audit"
	"github.com/bemeek-io/pando/internal/core/security"
	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/core/state"
)

// The security score, in the deploy path (R-312, R-314, design 09 §4.1).
//
// Two things happen here and they are deliberately separate: the scan, which
// produces a fact about what was just built, and the decision, which is host
// policy's. A deploy that is refused was scanned; a deploy on an installation
// with no threshold is scanned too, because the number is worth having before
// anybody sets one.

func (r *Runner) scan(ctx context.Context, dep state.Deployment, appSpec *spec.AppSpec, image, sourceDir string, sink io.Writer) error {
	if r.security == nil {
		return nil
	}
	if _, configured := r.security.Configured(); !configured {
		return nil
	}

	fmt.Fprintln(sink, "=> Scanning for known vulnerabilities")

	scanned, err := r.security.Scan(ctx, api.ScanRequest{
		AppID:     dep.AppID,
		SpecID:    dep.SpecID,
		Image:     image,
		SourceDir: sourceDir,
	}, audit.Event{
		PrincipalKind: audit.KindUser,
		PrincipalID:   dep.CreatedBy,
		OnBehalfOf:    dep.CreatedBy,
	})
	if err != nil {
		// R-318: a scanner that could not run does not block a deploy. The app
		// is not insecure because Pando could not look — it is unscanned, which
		// is recorded, shown, and left to the threshold check below, where an
		// app with an older passing scan still passes.
		fmt.Fprintf(sink, "   The scan did not run: %s\n", messageOf(err))
	} else if scanned.Score != nil {
		counts := security.Count(scanned.Findings)
		fmt.Fprintf(sink, "   Score %d — %d critical, %d high, %d medium, %d low\n",
			*scanned.Score, counts.Critical, counts.High, counts.Medium, counts.Low+counts.Unknown)
	}

	standing, err := r.security.Allows(ctx, dep.AppID, dep.SpecID)
	if err != nil {
		return err
	}
	if standing.Deployable() {
		return nil
	}

	return security.Refusal(standing, security.Worst(scanned.Findings, 3))
}
