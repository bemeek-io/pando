package deploy

import (
	"context"
	"fmt"
	"io"
	"time"

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

func (r *Runner) scan(ctx context.Context, dep state.Deployment, appSpec *spec.AppSpec, image, sourceDir, commit string, sink io.Writer) error {
	if r.security == nil {
		return nil
	}
	if _, configured := r.security.Configured(); !configured {
		return nil
	}

	// Scanned once per source, not once per deploy. Detection scans the
	// commit it read, a person can ask for a scan, and a new commit gets one
	// here — but a deploy of a commit already scanned uses that scan. It was
	// scanning the same source again minutes after the plan had, while the
	// person watched. The threshold below is still checked either way.
	if at, found, err := r.security.ScannedAt(ctx, dep.AppID, commit); err == nil && found {
		fmt.Fprintf(sink, "=> Using the security scan of %s from %s\n", short(commit), at.UTC().Format(time.RFC3339))
		return r.allowed(ctx, dep, nil)
	}

	fmt.Fprintln(sink, "=> Scanning for known vulnerabilities")

	scanned, err := r.security.Scan(ctx, api.ScanRequest{
		AppID:     dep.AppID,
		SpecID:    dep.SpecID,
		Commit:    commit,
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

	return r.allowed(ctx, dep, scanned.Findings)
}

// allowed is host policy's decision on the app's standing (R-314): the scan
// just taken, or the one this deploy reused.
func (r *Runner) allowed(ctx context.Context, dep state.Deployment, findings []api.Finding) error {
	standing, err := r.security.Allows(ctx, dep.AppID, dep.SpecID)
	if err != nil {
		return err
	}
	if standing.Deployable() {
		return nil
	}
	return security.Refusal(standing, security.Worst(findings, 3))
}
