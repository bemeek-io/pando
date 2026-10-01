// The app's newest spec revision, which an edit is made on top of.
//
// Newest rather than pinned, as the build plan does: an edit writes a new
// revision and leaves the pinned one alone (R-152), so a second edit made on
// the pinned one would quietly drop the first. The query keys are the ones the
// rest of the app's screen already uses, so this costs no extra requests.

import { useQuery } from '@tanstack/react-query';

import { api } from '@api/client';
import type { AppSpec } from '@api/types.gen';

interface Revision {
  id: string;
  revision: number;
  body: AppSpec;
}

export function useNewestSpec(appID: string) {
  const specs = useQuery({
    queryKey: ['apps', appID, 'specs'],
    queryFn: () => api.get<{ revisions: Revision[] | null; pinned_spec_id?: string }>(`/apps/${appID}/specs`),
  });
  const newest = (specs.data?.revisions ?? []).slice().sort((a, b) => b.revision - a.revision)[0];
  const full = useQuery({
    queryKey: ['apps', appID, 'spec', newest?.revision],
    queryFn: () => api.get<Revision>(`/apps/${appID}/specs/${newest?.revision}`),
    enabled: Boolean(newest),
  });
  return {
    spec: full.data?.body,
    /** Whether the newest revision is the one running, or an edit not yet deployed. */
    pinned: Boolean(newest && specs.data?.pinned_spec_id === newest.id),
    pending: specs.isPending || (Boolean(newest) && full.isPending),
  };
}
