package cli

import (
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/spf13/cobra"
)

// auditCmd reads the audit log (R-027), with the same filters as the console
// and the API, which it had none of: GET /audit was reachable from the console
// alone (R-261).
func auditCmd(client func() (*Client, error)) *cobra.Command {
	var action, actor, app, targetKind, target, since, until string
	var limit int

	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Read the audit log",
		Long: "Lists what was done on this installation, newest first. The filters combine.\n\n" +
			"--since and --until take a time (2026-09-21T09:00:00Z) or a duration back from now\n" +
			"(24h, 30m), so --since 24h is the last day.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			q := url.Values{}
			for key, v := range map[string]string{
				"action": action, "principal_id": actor, "app_id": app,
				"target_kind": targetKind, "target_id": target,
			} {
				if v != "" {
					q.Set(key, v)
				}
			}
			for key, v := range map[string]string{"since": since, "until": until} {
				if v == "" {
					continue
				}
				at, err := whenFlag(v)
				if err != nil {
					return fmt.Errorf("--%s: %w", key, err)
				}
				q.Set(key, at)
			}
			if limit > 0 {
				q.Set("limit", strconv.Itoa(limit))
			}

			var out struct {
				Events []struct {
					OccurredAt time.Time `json:"occurred_at"`
					Action     string    `json:"action"`
					Principal  string    `json:"principal_id"`
					OnBehalfOf string    `json:"on_behalf_of"`
					TargetKind string    `json:"target_kind"`
					TargetID   string    `json:"target_id"`
				} `json:"events"`
			}
			path := "/audit"
			if len(q) > 0 {
				path += "?" + q.Encode()
			}
			if err := c.Do("GET", path, nil, &out); err != nil {
				return err
			}

			t := table(cmd.OutOrStdout(), "WHEN", "ACTION", "WHO", "TARGET")
			for _, e := range out.Events {
				who := e.Principal
				if e.OnBehalfOf != "" && e.OnBehalfOf != e.Principal {
					who += " for " + e.OnBehalfOf
				}
				target := e.TargetID
				if e.TargetKind != "" {
					target = e.TargetKind + " " + target
				}
				fmt.Fprintf(t, "%s\t%s\t%s\t%s\n", e.OccurredAt.Local().Format(time.DateTime), e.Action, who, target)
			}
			return t.Flush()
		},
	}

	f := cmd.Flags()
	f.StringVar(&action, "action", "", "actions starting with this, e.g. app. or grant.delete")
	f.StringVar(&actor, "actor", "", "who did it: a user or token ID")
	f.StringVar(&app, "app", "", "events on this app")
	f.StringVar(&targetKind, "target-kind", "", "what kind of thing it was done to, e.g. user, role, app")
	f.StringVar(&target, "target", "", "the ID of the thing it was done to")
	f.StringVar(&since, "since", "", "from this time, or this long ago (24h)")
	f.StringVar(&until, "until", "", "up to this time, or this long ago")
	f.IntVar(&limit, "limit", 0, "how many events (default 100, at most 500)")
	return cmd
}

// whenFlag turns a time or a duration-ago into RFC 3339.
func whenFlag(v string) (string, error) {
	if d, err := time.ParseDuration(v); err == nil {
		return time.Now().Add(-d).UTC().Format(time.RFC3339), nil
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t.UTC().Format(time.RFC3339), nil
	}
	return "", fmt.Errorf("%q is neither a time like 2026-09-21T09:00:00Z nor a duration like 24h", v)
}
