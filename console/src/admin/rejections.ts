/**
 * The constructs Pando refused, read out of an error envelope's details.
 *
 * R-099 writes one entry per refusal — the service, the line, and why it cannot
 * cross the boundary — and the summary message ends "Each entry above says
 * why". Nothing rendered the entries, so "above" was one line naming a
 * construct and no reason anywhere.
 *
 * Details arrive as `Record<string, unknown>`: shaped by the server, typed by
 * nobody. Anything that is not three strings is dropped rather than rendered as
 * `[object Object]`.
 */
export interface Rejection {
  service: string;
  construct: string;
  reason: string;
}

export function rejectedEntries(details?: Record<string, unknown> | null): Rejection[] {
  const raw = details?.['rejected'];
  if (!Array.isArray(raw)) return [];

  return raw.flatMap((entry) => {
    if (typeof entry !== 'object' || entry === null) return [];
    const { service, construct, reason } = entry as Record<string, unknown>;
    if (typeof service !== 'string' || typeof construct !== 'string' || typeof reason !== 'string') {
      return [];
    }
    return [{ service, construct, reason }];
  });
}
