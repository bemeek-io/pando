// The security score (R-310 – R-320, design 09 §6).
//
// On the app's overview, with its status, address and deploy log: the score is a
// property of what is running, and the person who has to act on it is the one
// reading this page rather than somebody three tabs away in settings.
//
// A badge carrying the number, never a grade or a color standing alone (R-320).
// "F" tells a deployer nothing they can act on and a red pill tells somebody who
// cannot see red nothing at all — so the color is the second signal and the
// number is the first.
//
// The findings are the app's, so anyone who can view the app can read them. Only
// somebody who can deploy can ask for a new scan, because the score decides
// whether the next deploy is allowed.

import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Banner, Button, StatusIndicator, Table, Tag } from '@design';

import { api } from '@api/client';
import type { Finding, Report } from '@api/types.gen';
import { Quiet, messageOf } from '../install/Accounts';
import { MEASURE } from '../ui/layout';
import { relative } from '../ui/time';
import { ScoreBadge } from '../ui/ScoreBadge';

export function Security({ appID }: { appID: string }) {
  const queries = useQueryClient();

  const report = useQuery({
    queryKey: ['apps', appID, 'security'],
    queryFn: () => api.get<Report>(`/apps/${appID}/security`),
  });

  const scan = useMutation({
    mutationFn: () => api.post<Report>(`/apps/${appID}/security/scan`),
    onSuccess: () => queries.invalidateQueries({ queryKey: ['apps', appID, 'security'] }),
  });

  const [all, setAll] = useState(false);

  const standing = report.data?.standing;
  const counts = report.data?.counts;
  const findings = report.data?.scan?.findings ?? [];

  // The five that cost the most, ranked by the server. A base image can carry
  // two hundred findings, and a list of two hundred on the page somebody
  // deploys from is a list nobody reads and a Deploy button nobody can find.
  // The rest are one click away, in a box with a bottom to it.
  const worst = report.data?.worst ?? findings.slice(0, 5);
  const shown = all ? findings : worst;
  const hidden = findings.length - worst.length;

  return (
    <section style={{ maxWidth: MEASURE }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--space-4)' }}>
        <h4 style={{ font: 'var(--type-h4)', margin: 0 }}>Security</h4>
        <Button onClick={() => scan.mutate()} disabled={scan.isPending}>
          {scan.isPending ? 'Scanning' : 'Scan now'}
        </Button>
      </div>

      {report.isError && <Banner tone="failed">{messageOf(report.error)}</Banner>}
      {scan.isError && <Banner tone="failed">{messageOf(scan.error)}</Banner>}

      <div style={{ marginTop: 'var(--space-4)' }}>
        {standing && <Verdict report={report.data as Report} />}
      </div>

      {report.data?.scan?.error && (
        <div style={{ marginTop: 'var(--space-3)' }}>
          {/* R-318: a scanner that could not run is a thing to fix, and is not
              the same as an app that is clean. */}
          <Banner tone="failed">{report.data.scan.error}</Banner>
        </div>
      )}

      {counts && findings.length > 0 && (
        <div style={{ marginTop: 'var(--space-5)' }}>
          <div
            style={
              all
                ? {
                    // Bounded and scrolling, for the same reason the log is:
                    // a page that grows with its content pushes everything
                    // below it out of reach.
                    maxHeight: '50vh',
                    overflowY: 'auto',
                    overscrollBehavior: 'contain',
                  }
                : undefined
            }
          >
          <Table
            columns={[
              { key: 'id', header: 'Finding', width: 'minmax(0,24ch)', mono: true },
              {
                key: 'severity',
                header: 'Severity',
                width: '14ch',
                render: (row: Finding) => (
                  <StatusIndicator status={symbolFor(row.severity)} label={sentence(row.severity)} />
                ),
              },
              {
                key: 'title',
                header: 'What it is',
                width: 'minmax(0,40ch)',
                render: (row: Finding) => (
                  <span style={{ whiteSpace: 'normal', display: 'block', padding: 'var(--space-2) 0' }}>
                    {row.title}
                  </span>
                ),
              },
              {
                key: 'fix',
                header: 'Fixed in',
                width: '18ch',
                muted: true,
                render: (row: Finding) => (row.fix ? <Tag mono>{row.fix}</Tag> : 'No fix yet'),
              },
            ]}
            rows={shown}
          />
          </div>

          {hidden > 0 && (
            <div style={{ marginTop: 'var(--space-3)' }}>
              <Button variant="ghost" onClick={() => setAll(!all)}>
                {all ? 'Show the five worst' : `Show all ${findings.length} findings`}
              </Button>
            </div>
          )}
        </div>
      )}
    </section>
  );
}

function Verdict({ report }: { report: Report }) {
  const { standing, counts, scanner } = report;
  const score = standing.score;

  if (standing.verdict === 'inert') {
    return (
      <Quiet>
        This installation requires a security score of {standing.threshold}, and has no scanner
        configured — so nothing is scored and nothing is enforced.
      </Quiet>
    );
  }

  if (score === null || score === undefined) {
    return (
      <Quiet>
        {scanner
          ? 'This app has not been scanned yet.'
          : 'This installation does not scan apps, so there is no score.'}
      </Quiet>
    );
  }

  const taken = report.scan?.ran_at ? relative(report.scan.ran_at) : '';

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-2)' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--space-3)' }}>
        <ScoreBadge score={score} verdict={standing.verdict as never} threshold={standing.threshold} full />
        <StatusIndicator
          status={standing.verdict === 'insecure' ? 'failed' : 'running'}
          label={
            standing.verdict === 'insecure'
              ? `Below this installation's requirement of ${standing.threshold}`
              : standing.threshold > 0
                ? `Meets this installation's requirement of ${standing.threshold}`
                : 'No requirement is set'
          }
        />
      </div>

      <Quiet>
        {counts.critical} critical, {counts.high} high, {counts.medium} medium,{' '}
        {counts.low + counts.unknown} low
        {taken ? ` · scanned ${taken.toLowerCase()}` : ''}
      </Quiet>

      {standing.verdict === 'insecure' && standing.stop_at && (
        // R-316: the deadline, at the moment it starts, to the person who has
        // to act on it.
        <Banner tone="failed">
          Pando will stop this app on {new Date(standing.stop_at).toLocaleString()} unless its score
          reaches {standing.threshold}.
        </Banner>
      )}
    </div>
  );
}

/** Severity onto the design system's symbols. Never color alone. */
function symbolFor(severity: string): 'failed' | 'building' | 'stopped' | 'info' {
  switch (severity) {
    case 'critical':
    case 'high':
      return 'failed';
    case 'medium':
      return 'building';
    case 'low':
      return 'stopped';
    default:
      return 'info';
  }
}

function sentence(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1);
}
