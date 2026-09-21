// The audit log's actor filter: type to find an account, rather than scroll a
// list of every one.
//
// Matches username, name, email and ID, and offers the first few. Choosing one
// filters by its ID and shows its username. A typed ID — usr_… or tok_…, which
// is also how a token or a service account is found, and all a reader without
// install.view can do — is used as is on Enter. Clearing the field clears the
// filter.

import { useEffect, useRef, useState } from 'react';
import { Input } from '@design';

import { matches } from '../ui/search';

export interface Person {
  id: string;
  external_id: string;
  display_name?: string;
  email?: string;
}

const SHOWN = 8;

export function ActorField({
  people,
  value,
  onChange,
}: {
  people: Person[];
  /** The chosen principal ID, or empty. */
  value: string;
  onChange: (principalID: string) => void;
}) {
  const nameOf = (id: string) => people.find((p) => p.id === id)?.external_id ?? id;
  const [text, setText] = useState(() => (value ? nameOf(value) : ''));
  const [open, setOpen] = useState(false);
  const [at, setAt] = useState(0);
  const root = useRef<HTMLDivElement>(null);

  // Cleared from outside — "Clear filters" — clears the text too.
  useEffect(() => {
    if (!value) setText('');
  }, [value]);

  useEffect(() => {
    if (!open) return undefined;
    const outside = (e: MouseEvent) => {
      if (!root.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener('mousedown', outside);
    return () => document.removeEventListener('mousedown', outside);
  }, [open]);

  const found = text.trim()
    ? people.filter((p) => matches(text, p.external_id, p.display_name, p.email, p.id)).slice(0, SHOWN)
    : [];

  const choose = (p: Person) => {
    setText(p.external_id);
    setOpen(false);
    onChange(p.id);
  };

  return (
    <div ref={root} style={{ position: 'relative' }}>
      <Input
        label="Actor"
        mono
        role="combobox"
        aria-expanded={open && found.length > 0}
        aria-autocomplete="list"
        placeholder="Username, email or ID"
        value={text}
        onChange={(e) => {
          setText(e.target.value);
          setAt(0);
          setOpen(true);
          if (!e.target.value.trim()) onChange('');
        }}
        onFocus={() => setOpen(true)}
        onKeyDown={(e) => {
          if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
            e.preventDefault();
            if (found.length === 0) return;
            setOpen(true);
            setAt((i) => (e.key === 'ArrowDown' ? (i + 1) % found.length : (i - 1 + found.length) % found.length));
          } else if (e.key === 'Enter') {
            e.preventDefault();
            const typed = text.trim();
            const exact = people.find((p) => p.id === typed || p.external_id === typed);
            if (open && found[at]) choose(found[at]);
            else if (exact) choose(exact);
            else if (/^(usr|tok)_/.test(typed)) {
              setOpen(false);
              onChange(typed);
            }
          } else if (e.key === 'Escape') {
            setOpen(false);
          }
        }}
      />
      {open && found.length > 0 && (
        <div
          role="listbox"
          aria-label="Matching accounts"
          style={{
            position: 'absolute',
            top: '100%',
            left: 0,
            right: 0,
            zIndex: 20,
            marginTop: 'var(--space-1)',
            padding: 'var(--space-1)',
            background: 'var(--paper-raised)',
            border: 'var(--border-width) solid var(--rule-strong)',
            borderRadius: 'var(--radius-md)',
            boxShadow: 'var(--shadow-popover)',
          }}
        >
          {found.map((p, i) => (
            <div
              key={p.id}
              role="option"
              aria-selected={i === at}
              onMouseDown={(e) => {
                // Before the input blurs, so the choice lands.
                e.preventDefault();
                choose(p);
              }}
              onMouseEnter={() => setAt(i)}
              style={{
                display: 'flex',
                justifyContent: 'space-between',
                gap: 'var(--space-3)',
                padding: 'var(--space-2) var(--space-3)',
                borderRadius: 'var(--radius-sm)',
                cursor: 'pointer',
                background: i === at ? 'var(--paper-sunken)' : 'transparent',
                font: 'var(--type-body-ui)',
                color: 'var(--ink)',
              }}
            >
              <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{p.external_id}</span>
              <span
                style={{
                  color: 'var(--ink-secondary)',
                  overflow: 'hidden',
                  textOverflow: 'ellipsis',
                  whiteSpace: 'nowrap',
                }}
              >
                {p.display_name || p.email}
              </span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
