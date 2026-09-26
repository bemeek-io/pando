// API and tools (R-261, R-262).
//
// Everything Pando does, it does through the API; the console, the CLI and MCP
// are clients of it. This screen is that sentence made usable: how to connect,
// a token to connect with, and the whole surface of all three.
//
// It renders `GET /api/v1/reference`, which the running binary builds from its
// own router, its own cobra tree and its own MCP tool list. Nothing on this
// screen is written here — a page describing the API that was maintained by
// hand would be wrong the first week and trusted for months. `docs/api.md`,
// `docs/cli.md` and `docs/mcp.md` are the same document, written by
// `make reference` and checked in CI.
//
// Open to anyone signed in. A developer with no administrative verb still has
// apps shared with them, still automates, and still needs a token; hiding the
// manual behind administration would make the API an administrative feature,
// which R-262 explicitly says it is not.

import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Banner, Button, CodeBlock, Dialog, InlineCode, Input, Skeleton, Tabs, Tag } from '@design';

import { api } from '@api/client';
import type { Command, Document, Route, Token } from '@api/types.gen';
import { InstallVerb, useInstallVerb } from '../app/principal';
import { Quiet, Screen, messageOf } from '../install/Accounts';
import { MEASURE } from '../ui/layout';
import { relative } from '../ui/time';
import { Table } from '../ui/Table';
import { LineSkeleton, Loading } from '../ui/Loading';
import { AnsweredBy, AskAI } from '../ui/AskAI';
import { useAIFunctionState } from '../install/AIFunctions';

/** What POST /ai/reference/answer answers (R-346). */
interface ReferenceAnswer {
  answer: string;
  cites: string[];
  covered: boolean;
  adapter_id?: string;
  model?: string;
}

/**
 * "How can I…", answered from this same reference (R-346). The answer
 * describes and cites; the tabs below are where to check it. Offered to
 * everyone signed in unless reference help is known to be off.
 */
function AskHow() {
  const state = useAIFunctionState('answer_reference');
  const ask = useMutation({
    mutationFn: (question: string) => api.post<ReferenceAnswer>('/ai/reference/answer', { question }),
  });
  if (state === 'off') return null;
  const a = ask.data;
  return (
    <div style={{ maxWidth: MEASURE, display: 'flex', flexDirection: 'column', gap: 'var(--space-2)', marginBottom: 'var(--space-5)' }}>
      <AskAI
        label="Ask how to do something"
        placeholder="How can I give a group access to one app?"
        pending={ask.isPending}
        error={ask.error}
        onAsk={(q) => ask.mutate(q)}
      />
      {a && (
        <>
          <p style={{ font: 'var(--type-body-ui)', margin: 0, whiteSpace: 'pre-wrap' }}>{a.answer}</p>
          {a.cites.length > 0 && (
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 'var(--space-2)' }}>
              {a.cites.map((c) => (
                <InlineCode key={c}>{c}</InlineCode>
              ))}
            </div>
          )}
          <AnsweredBy adapter={a.adapter_id} model={a.model} />
        </>
      )}
    </div>
  );
}

export function Reference() {
  const [tab, setTab] = useState('connect');

  const doc = useQuery({
    queryKey: ['reference'],
    queryFn: () => api.get<Document>('/reference'),
    // The reference changes when the binary changes, which is not while
    // somebody is reading it.
    staleTime: 60 * 60 * 1000,
  });

  return (
    <Screen heading="API and tools">
      {doc.isError && <Banner tone="failed">{messageOf(doc.error)}</Banner>}

      <AskHow />

      <Tabs
        value={tab}
        onChange={setTab}
        items={[
          { value: 'connect', label: 'Connect' },
          { value: 'tokens', label: 'Tokens' },
          { value: 'api', label: 'API' },
          { value: 'cli', label: 'CLI' },
          { value: 'mcp', label: 'MCP' },
          { value: 'errors', label: 'Errors' },
        ]}
      />

      <div style={{ marginTop: 'var(--space-5)' }}>
        {/* Every tab but Tokens renders the reference, so until it arrives
            they share one outline: a heading and a code block, which is how
            each of them opens. */}
        {doc.isPending && tab !== 'tokens' && (
          <Loading>
            <div style={{ maxWidth: MEASURE, display: 'flex', flexDirection: 'column', gap: 'var(--space-3)' }}>
              <LineSkeleton width="16ch" font="var(--type-h4)" />
              <Skeleton height="6rem" radius="md" />
              <LineSkeleton width="48ch" />
            </div>
          </Loading>
        )}
        {tab === 'connect' && <Connect doc={doc.data} />}
        {tab === 'tokens' && <Tokens />}
        {tab === 'api' && <API doc={doc.data} />}
        {tab === 'cli' && <CLI doc={doc.data} />}
        {tab === 'mcp' && <MCP doc={doc.data} />}
        {tab === 'errors' && <Errors doc={doc.data} />}
      </div>
    </Screen>
  );
}

// --- connect ---------------------------------------------------------------

// Without the reference, each surface renders nothing: while it loads the
// screen shows its outline, and when it failed the banner above says why. The
// bare "Loading." these used to show stayed up for good after a failure.
function Connect({ doc }: { doc?: Document }) {
  if (!doc) return null;

  // Where this console is, which is where the API is. Written out rather than
  // left as "your server": the whole point of this screen is that it can be
  // copied into a terminal without editing.
  const origin = window.location.origin;

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-7)', maxWidth: MEASURE }}>
      <section>
        <h4 style={{ font: 'var(--type-h4)', margin: '0 0 var(--space-3)' }}>The API</h4>
        <CodeBlock
          lines={[
            `curl -H "Authorization: Bearer $${doc.connect.token_env}" \\`,
            `  ${origin}${doc.api.base_path}/apps`,
          ]}
        />
      </section>

      <section>
        <h4 style={{ font: 'var(--type-h4)', margin: '0 0 var(--space-3)' }}>Install the CLI</h4>
        <Quiet>
          The same binary as the server, on your machine, talking to this installation over its API.
        </Quiet>
        <div style={{ marginTop: 'var(--space-3)', display: 'flex', flexDirection: 'column', gap: 'var(--space-3)' }}>
          <CodeBlock title="macOS, and Linux with Homebrew" lines={[`brew install ${doc.install.homebrew}`]} prompt />
          <CodeBlock
            title={`Debian and Ubuntu — ${(doc.install.packages ?? [])
              .filter((p) => p !== 'deb')
              .map((p) => `.${p}`)
              .join(' and ')} are published the same way`}
            lines={packageLines(doc)}
            prompt
          />
          <CodeBlock title="From source" lines={[`go install ${doc.install.module}@latest`]} prompt />
          <CodeBlock
            title="Or nothing at all — the installation already has it"
            lines={[`${doc.install.in_container} app list`]}
            prompt
          />
        </div>
        <div style={{ marginTop: 'var(--space-3)' }}>
          <Quiet>
            Tarballs named <code style={{ font: 'var(--type-code-sm)' }}>{doc.install.archive}</code>{' '}
            are published for {(doc.install.platforms ?? []).join(', ')}. Every release, with its
            signed checksums, is on the{' '}
            <a href={`${doc.install.repo}/releases`} target="_blank" rel="noopener noreferrer">
              releases page
            </a>
            .
          </Quiet>
        </div>
      </section>

      <section>
        <h4 style={{ font: 'var(--type-h4)', margin: '0 0 var(--space-3)' }}>Sign the CLI in</h4>
        <CodeBlock lines={[doc.connect.login_cmd.replace('<server-url>', origin)]} prompt />
        <div style={{ marginTop: 'var(--space-4)' }}>
          <Quiet>
            For a machine — CI, a container, a script — use a token from the Tokens tab instead of
            signing in.
          </Quiet>
          <div style={{ marginTop: 'var(--space-3)' }}>
            <CodeBlock
              lines={[
                `export ${doc.connect.server_env}=${origin}`,
                `export ${doc.connect.token_env}=tok_…`,
              ]}
            />
          </div>
        </div>
      </section>

      <section>
        <h4 style={{ font: 'var(--type-h4)', margin: '0 0 var(--space-3)' }}>MCP</h4>
        <CodeBlock
          lines={[
            '{',
            '  "mcpServers": {',
            '    "pando": {',
            '      "command": "pando",',
            `      "args": ["${doc.connect.mcp_cmd.replace(/^pando /, '')}"],`,
            '      "env": {',
            `        "${doc.connect.server_env}": "${origin}",`,
            `        "${doc.connect.token_env}": "tok_…"`,
            '      }',
            '    }',
            '  }',
            '}',
          ]}
        />
      </section>

      <section>
        <h4 style={{ font: 'var(--type-h4)', margin: '0 0 var(--space-3)' }}>Authenticating</h4>
        {doc.api.auth?.map((auth) => (
          <div key={auth.name} style={{ marginBottom: 'var(--space-4)' }}>
            <p style={{ font: 'var(--type-body-ui)', margin: '0 0 var(--space-1)' }}>
              <strong>{auth.name}</strong> · <code style={{ font: 'var(--type-code-sm)' }}>{auth.how}</code>
            </p>
            <Quiet>{auth.description}</Quiet>
          </div>
        ))}
      </section>
    </div>
  );
}

/**
 * The download-and-install lines, spelled out.
 *
 * An instruction with three angle brackets in it is one somebody has to
 * assemble before they can run it, and assembling it is where they get it
 * wrong. The version is the one variable, at the top.
 */
function packageLines(doc: Document): string[] {
  const file = doc.install.package
    .replace('<format>', 'deb')
    .replace('<arch>', 'amd64')
    .replace('<version>', '${VERSION}');
  const url = doc.install.download.replace('<version>', '${VERSION}') + file;
  return [
    'VERSION=0.2.0   # the release you want',
    `curl -LO ${url}`,
    `sudo apt install ./${file}`,
  ];
}

// --- tokens ----------------------------------------------------------------

function Tokens() {
  // Service tokens have their own verb (R-080): issuing a credential for an
  // automation is not the same trust as managing people.
  const canManageTokens = useInstallVerb(InstallVerb.TokensManage);

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-7)', maxWidth: MEASURE }}>
      <TokenList
        heading="Your tokens"
        // R-058, R-059, said where somebody is about to make one.
        note="A token acts as you and holds exactly what you hold. If you lose access to an app, so does the token. If your account goes, the token goes with it."
        path="/tokens"
        queryKey={['tokens']}
      />

      {canManageTokens && (
        <TokenList
          heading="Service tokens"
          // R-060: the difference that matters, in the place where the choice
          // between the two is made.
          note="A service token is its own principal. It holds nothing until somebody shares an app with it, it appears in the audit log under its own name, and it outlives whoever created it."
          path="/tokens/service"
          queryKey={['tokens', 'service']}
        />
      )}
    </div>
  );
}

function TokenList({
  heading,
  note,
  path,
  queryKey,
}: {
  heading: string;
  note: string;
  path: string;
  queryKey: string[];
}) {
  const queries = useQueryClient();
  const [creating, setCreating] = useState(false);

  const tokens = useQuery({
    queryKey,
    queryFn: () => api.get<{ tokens: Token[] | null }>(path),
  });

  const revoke = useMutation({
    mutationFn: (id: string) => api.del(`/tokens/${id}`),
    onSuccess: () => queries.invalidateQueries({ queryKey }),
  });

  const rows = (tokens.data?.tokens ?? []).filter((t) => !t.revoked_at);

  return (
    <section>
      <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--space-4)' }}>
        <h4 style={{ font: 'var(--type-h4)', margin: 0 }}>{heading}</h4>
        <Button onClick={() => setCreating(true)}>New token</Button>
      </div>
      <div style={{ marginTop: 'var(--space-2)' }}>
        <Quiet>{note}</Quiet>
      </div>

      {tokens.isError && <Banner tone="failed">{messageOf(tokens.error)}</Banner>}

      <div style={{ marginTop: 'var(--space-4)' }}>
        <Table
          loading={tokens.isPending}
          skeletonRows={2}
          columns={[
            { key: 'name', header: 'Name', width: 'minmax(0,30ch)' },
            {
              key: 'expires_at',
              header: 'Expires',
              width: '18ch',
              muted: true,
              render: (row: Token) =>
                row.expires_at ? new Date(row.expires_at).toLocaleDateString() : 'Never',
            },
            {
              key: 'last_used_at',
              header: 'Last used',
              width: '18ch',
              muted: true,
              render: (row: Token) => (row.last_used_at ? relative(row.last_used_at) : 'Never'),
            },
            {
              key: 'actions',
              header: '',
              width: '18ch',
              align: 'right',
              render: (row: Token) => (
                <Button
                  variant="destructive"
                  disabled={revoke.isPending}
                  onClick={() => revoke.mutate(row.id)}
                >
                  Revoke
                </Button>
              ),
            },
          ]}
          rows={rows}
          empty={<Quiet>No tokens.</Quiet>}
        />
      </div>

      {revoke.isError && <Banner tone="failed">{messageOf(revoke.error)}</Banner>}

      {creating && (
        <NewToken
          heading={heading}
          path={path}
          onClose={() => setCreating(false)}
          onCreated={() => void queries.invalidateQueries({ queryKey })}
        />
      )}
    </section>
  );
}

/** Expiries offered. A number of days, because that is what the API takes. */
const LIFETIMES = [
  { value: 30, label: '30 days' },
  { value: 90, label: '90 days' },
  { value: 365, label: 'A year' },
  { value: 0, label: 'Never' },
];

function NewToken({
  heading,
  path,
  onClose,
  onCreated,
}: {
  heading: string;
  path: string;
  onClose: () => void;
  onCreated: () => void;
}) {
  const [name, setName] = useState('');
  const [days, setDays] = useState(90);
  const [secret, setSecret] = useState<string | null>(null);

  const create = useMutation({
    mutationFn: () => api.post<{ secret: string }>(path, { name, expires_days: days }),
    onSuccess: (issued) => {
      setSecret(issued.secret);
      onCreated();
    },
  });

  // R-063: shown once. The dialog does not close on success — closing it would
  // take the only copy of the secret with it.
  if (secret) {
    return (
      <Dialog
        open
        onClose={onClose}
        title="Copy this now"
        description="This is the only time Pando will show it."
        footer={
          <Button variant="primary" onClick={onClose}>
            Done
          </Button>
        }
      >
        <CodeBlock lines={[secret]} />
      </Dialog>
    );
  }

  return (
    <Dialog
      open
      onClose={onClose}
      title={heading === 'Service tokens' ? 'New service token' : 'New token'}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" disabled={create.isPending || !name} onClick={() => create.mutate()}>
            {create.isPending ? 'Creating' : 'Create'}
          </Button>
        </>
      }
    >
      <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-5)' }}>
        <Input
          label="Name"
          value={name}
          helper="Where it will be used. This is what appears in the audit log."
          onChange={(e) => setName(e.target.value)}
        />

        <div>
          <p style={{ font: 'var(--type-label)', margin: '0 0 var(--space-2)' }}>Expires</p>
          <div style={{ display: 'flex', gap: 'var(--space-2)', flexWrap: 'wrap' }}>
            {LIFETIMES.map((option) => (
              <Button
                key={option.value}
                variant={days === option.value ? 'primary' : 'secondary'}
                onClick={() => setDays(option.value)}
              >
                {option.label}
              </Button>
            ))}
          </div>
          {days === 0 && (
            <div style={{ marginTop: 'var(--space-3)' }}>
              {/* R-061: host policy may cap this, and the server clamps rather
                  than refusing. Saying so beats a token that quietly expires
                  in ninety days. */}
              <Quiet>Host policy may still put an expiry on it.</Quiet>
            </div>
          )}
        </div>

        {create.isError && <Banner tone="failed">{messageOf(create.error)}</Banner>}
      </div>
    </Dialog>
  );
}

// --- the surfaces ----------------------------------------------------------

function API({ doc }: { doc?: Document }) {
  if (!doc) return null;

  const routes = doc.api.routes ?? [];
  const groups = [...new Set(routes.map((r) => r.group))];

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-7)' }}>
      <Quiet>
        {routes.length} endpoints, read from this installation. The verb is what the endpoint asks
        for; blank means it asks only that you are signed in.
      </Quiet>

      {groups.map((group) => (
        <section key={group}>
          <h4 style={{ font: 'var(--type-h4)', margin: '0 0 var(--space-3)' }}>{group}</h4>
          <Table
            columns={[
              {
                key: 'path',
                header: 'Endpoint',
                width: 'minmax(0,46ch)',
                mono: true,
                render: (row: Route) => `${row.method} ${row.path}`,
              },
              { key: 'verb', header: 'Verb', width: '22ch', mono: true, muted: true },
              {
                key: 'summary',
                header: 'What it does',
                width: 'minmax(0,60ch)',
                render: (row: Route) => (
                  <span style={{ whiteSpace: 'normal', display: 'block', padding: 'var(--space-2) 0' }}>
                    {row.summary}
                  </span>
                ),
              },
            ]}
            rows={routes.filter((r) => r.group === group)}
          />
        </section>
      ))}
    </div>
  );
}

function CLI({ doc }: { doc?: Document }) {
  if (!doc) return null;

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-6)', maxWidth: MEASURE }}>
      {(doc.cli ?? []).map((command) => (
        <CommandDoc key={command.name} command={command} />
      ))}
    </div>
  );
}

function CommandDoc({ command, depth = 0 }: { command: Command; depth?: number }) {
  return (
    <section style={{ marginLeft: depth ? 'var(--space-5)' : 0 }}>
      <div style={{ display: 'flex', alignItems: 'baseline', gap: 'var(--space-3)' }}>
        <code style={{ font: 'var(--type-code)', color: 'var(--ink)' }}>{command.use}</code>
      </div>
      {command.summary && (
        <div style={{ marginTop: 'var(--space-1)' }}>
          <Quiet>{command.summary}</Quiet>
        </div>
      )}
      {(command.flags ?? []).length > 0 && (
        <div style={{ marginTop: 'var(--space-2)', display: 'flex', gap: 'var(--space-2)', flexWrap: 'wrap' }}>
          {(command.flags ?? []).map((flag) => (
            <Tag key={flag.name} mono>
              --{flag.name}
            </Tag>
          ))}
        </div>
      )}
      {(command.children ?? []).length > 0 && (
        <div style={{ marginTop: 'var(--space-4)', display: 'flex', flexDirection: 'column', gap: 'var(--space-4)' }}>
          {(command.children ?? []).map((child) => (
            <CommandDoc key={child.name} command={child} depth={depth + 1} />
          ))}
        </div>
      )}
    </section>
  );
}

function MCP({ doc }: { doc?: Document }) {
  if (!doc) return null;

  return (
    <div>
      <Quiet>
        One tool per endpoint. An agent holds a token and is a principal like any other: it acts as
        its owner, is bounded by their grants, and its actions are in the audit log under their name.
      </Quiet>
      <div style={{ marginTop: 'var(--space-4)' }}>
        <Table
          columns={[
            { key: 'name', header: 'Tool', width: 'minmax(0,30ch)', mono: true },
            {
              key: 'description',
              header: 'What it does',
              width: 'minmax(0,60ch)',
              render: (row: { description: string }) => (
                <span style={{ whiteSpace: 'normal', display: 'block', padding: 'var(--space-2) 0' }}>
                  {row.description}
                </span>
              ),
            },
          ]}
          rows={doc.mcp ?? []}
        />
      </div>
    </div>
  );
}

function Errors({ doc }: { doc?: Document }) {
  if (!doc) return null;

  return (
    <div>
      <Quiet>
        Every error carries a stable code, a message, an optional remedy and the request ID that
        finds the log line. Branch on the code; the message may be reworded.
      </Quiet>
      <div style={{ marginTop: 'var(--space-4)' }}>
        <Table
          columns={[
            { key: 'code', header: 'Code', width: 'minmax(0,34ch)', mono: true },
            { key: 'status', header: 'HTTP', width: '10ch', muted: true },
            {
              key: 'meaning',
              header: 'Meaning',
              width: 'minmax(0,60ch)',
              render: (row: { meaning: string }) => (
                <span style={{ whiteSpace: 'normal', display: 'block', padding: 'var(--space-2) 0' }}>
                  {row.meaning}
                </span>
              ),
            },
          ]}
          rows={doc.errors ?? []}
        />
      </div>
    </div>
  );
}
