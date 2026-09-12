// A terminal inside a running workload (R-084, R-086).
//
// The copy on this screen is doing real work. R-086 says the documentation must
// state plainly that exec is the highest-privilege action in the system and that
// the verb list is **not** a security boundary against someone holding it — a
// holder can read the database directly and read injected environment including
// secrets. So the screen says that, before opening anything, rather than
// implying a terminal is just another tab.
//
// It also says what is recorded: the command, not the session (O-7). Someone
// about to type a password into a shell is entitled to know which of those two
// ends up in the audit log.

import { useEffect, useRef, useState } from 'react';
import { FitAddon } from '@xterm/addon-fit';
import { Terminal as Xterm } from '@xterm/xterm';
import '@xterm/xterm/css/xterm.css';

import { Button, Card } from '@design';

export function Terminal({ appID, workload }: { appID: string; workload?: string }) {
  const [open, setOpen] = useState(false);

  if (!open) {
    return <Warning onOpen={() => setOpen(true)} />;
  }
  return <Session appID={appID} workload={workload} onClose={() => setOpen(false)} />;
}

/**
 * What exec actually grants, said before it is granted.
 *
 * Not a tooltip and not a dialog to dismiss: R-086 asks for this to be stated
 * plainly, and a dialog is a thing people click past.
 */
function Warning({ onOpen }: { onOpen: () => void }) {
  return (
    <Card padding="md" style={{ maxWidth: 'var(--console-max)' }}>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-3)' }}>
        <h4 style={{ font: 'var(--type-h4)', margin: 0 }}>Open a terminal in this app</h4>

        <p style={{ font: 'var(--type-body-ui)', color: 'var(--ink)', margin: 0 }}>
          A terminal gives you everything the app itself has. You can read its database directly,
          read the values Pando passed it — including its secrets — and change the running app in
          ways that won&rsquo;t show up in its configuration.
        </p>

        <p style={{ font: 'var(--type-body-ui)', color: 'var(--ink-secondary)', margin: 0 }}>
          Pando records that you opened a terminal and the command you started, but not what you
          type or what comes back.
        </p>

        <Button variant="primary" onClick={onOpen} style={{ alignSelf: 'flex-start' }}>
          Open terminal
        </Button>
      </div>
    </Card>
  );
}

function Session({
  appID,
  workload,
  onClose,
}: {
  appID: string;
  workload?: string;
  onClose: () => void;
}) {
  const host = useRef<HTMLDivElement>(null);
  const [status, setStatus] = useState<string>('Connecting.');

  useEffect(() => {
    const element = host.current;
    if (!element) return;

    // The terminal surface is identical in both themes (brand spec), so it
    // reads its colours from the terminal tokens rather than the page's.
    const styles = getComputedStyle(document.documentElement);
    const terminal = new Xterm({
      fontFamily: styles.getPropertyValue('--font-mono').trim() || 'monospace',
      fontSize: 13,
      cursorBlink: true,
      theme: {
        background: styles.getPropertyValue('--terminal').trim(),
        foreground: styles.getPropertyValue('--terminal-text').trim(),
        cursor: styles.getPropertyValue('--terminal-text').trim(),
      },
    });

    const fit = new FitAddon();
    terminal.loadAddon(fit);
    terminal.open(element);
    fit.fit();

    const query = workload ? `?workload=${encodeURIComponent(workload)}` : '';
    const scheme = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const socket = new WebSocket(
      `${scheme}//${window.location.host}/api/v1/apps/${appID}/exec${query}`,
    );
    socket.binaryType = 'arraybuffer';

    // Terminal bytes are binary frames; a resize is a text frame. Keeping them
    // apart is what stops a resize being typed into the user's shell.
    const sendSize = () => {
      fit.fit();
      if (socket.readyState === WebSocket.OPEN) {
        socket.send(JSON.stringify({ rows: terminal.rows, cols: terminal.cols }));
      }
    };

    socket.onopen = () => {
      setStatus('');
      sendSize();
    };

    socket.onmessage = (event) => {
      if (event.data instanceof ArrayBuffer) {
        terminal.write(new Uint8Array(event.data));
        return;
      }
      terminal.write(String(event.data));
    };

    socket.onclose = (event) => {
      // The server closes with a reason — "exited with status 1" — so the
      // terminal says how the shell ended rather than going blank.
      setStatus(event.reason ? `Session ended: ${event.reason}` : 'Session ended.');
    };

    socket.onerror = () => setStatus('Pando couldn’t open a terminal in this app.');

    const input = terminal.onData((data) => {
      if (socket.readyState === WebSocket.OPEN) {
        socket.send(new TextEncoder().encode(data));
      }
    });

    const observer = new ResizeObserver(sendSize);
    observer.observe(element);

    return () => {
      observer.disconnect();
      input.dispose();
      socket.close();
      terminal.dispose();
    };
  }, [appID, workload]);

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-3)' }}>
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          gap: 'var(--space-3)',
        }}
      >
        <span style={{ font: 'var(--type-caption)', color: 'var(--ink-secondary)' }}>{status}</span>
        <Button variant="ghost" onClick={onClose}>
          Close terminal
        </Button>
      </div>

      <div
        ref={host}
        style={{
          background: 'var(--terminal)',
          borderRadius: 'var(--radius-md)',
          padding: 'var(--space-3)',
          height: 'var(--space-10)',
          minHeight: 'var(--space-10)',
        }}
      />
    </div>
  );
}
