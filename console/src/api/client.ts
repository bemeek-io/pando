// The API client.
//
// R-261: the API is the product, and the console is one of its clients with no
// capability the API lacks. So this is deliberately thin — a fetch wrapper that
// understands the error envelope and nothing else. Business logic that appears
// here instead of behind an endpoint is a capability the CLI and MCP will not
// have.

import type { Error as ApiError } from './types.gen';

export type { ApiError };

/** Thrown for any non-2xx response, carrying the envelope when there is one. */
export class RequestFailed extends Error {
  readonly status: number;
  readonly envelope: ApiError | null;

  constructor(status: number, envelope: ApiError | null, fallback: string) {
    // The envelope's message is written to the R-105 standard — self-contained,
    // actionable, no apology — so it is shown as-is. Rewriting it here would
    // undo that in the UI layer, which is the same mistake as paraphrasing a
    // detection question.
    super(envelope?.message ?? fallback);
    this.name = 'RequestFailed';
    this.status = status;
    this.envelope = envelope;
  }

  /** The remedy, when the server supplied one. */
  get remedy(): string | undefined {
    return this.envelope?.remedy || undefined;
  }

  get code(): string | undefined {
    return this.envelope?.code || undefined;
  }
}

const base = '/api/v1';

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const response = await fetch(base + path, {
    method,
    headers: body === undefined ? {} : { 'Content-Type': 'application/json' },
    body: body === undefined ? null : JSON.stringify(body),
    credentials: 'same-origin',
  });

  if (!response.ok) {
    let envelope: ApiError | null = null;
    try {
      envelope = (await response.json()) as ApiError;
    } catch {
      // A response with no envelope is a failure the API did not expect to
      // produce. Reported as-is rather than dressed up as one it did.
      envelope = null;
    }
    throw new RequestFailed(response.status, envelope, `The request failed (${response.status}).`);
  }

  if (response.status === 204) return undefined as T;
  return (await response.json()) as T;
}

export const api = {
  get: <T>(path: string) => request<T>('GET', path),
  post: <T>(path: string, body?: unknown) => request<T>('POST', path, body),
  put: <T>(path: string, body?: unknown) => request<T>('PUT', path, body),
  patch: <T>(path: string, body?: unknown) => request<T>('PATCH', path, body),
  del: <T>(path: string) => request<T>('DELETE', path),
};
