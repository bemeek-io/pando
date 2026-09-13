// Adding an app — the screen R-002 is actually about.
//
// "Setup cost is paid once, at the host. Deploying the tenth app must feel like
// nothing." Every other screen in the console administers apps that already
// exist; until this one there was no way to make one except `pando app add` or
// a POST by hand, which fails R-005 outright — the person this is for may not
// know what a port is, and will certainly not be running curl.
//
// Design 08's principle governs the shape: **the default path shows almost
// nothing — name, source, deploy.** Everything with a sane default lives behind
// Advanced and is never surfaced during setup. A field here has to justify
// being a blocker rather than configuration.
//
// So the whole form is one input. The name is derived from the URL and shown as
// a filled field somebody may correct, rather than asked for — asking is how a
// two-field form becomes a four-field form.

import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Banner, Button, Dialog, Input, Select } from '@design';

import { api } from '@api/client';
import type { App } from '@api/types.gen';
import { messageOf } from '../install/Accounts';

type Kind = 'git' | 'image';

export function AddApp({ onAdded, onClose }: { onAdded: (app: App) => void; onClose: () => void }) {
  const queries = useQueryClient();

  const [kind, setKind] = useState<Kind>('git');
  const [url, setUrl] = useState('');
  const [image, setImage] = useState('');
  const [name, setName] = useState('');
  const [named, setNamed] = useState(false);

  const source = kind === 'git' ? url : image;

  const create = useMutation({
    mutationFn: () =>
      api.post<App>('/apps', {
        name: name.trim(),
        source: kind === 'git' ? { type: 'git', url: url.trim() } : { type: 'image', image: image.trim() },
      }),
    onSuccess: (app) => {
      void queries.invalidateQueries({ queryKey: ['apps'] });
      onAdded(app);
    },
  });

  // The name follows the source until somebody types one. Deriving it silently
  // and showing the result is the difference between one question and two.
  const edit = (set: (v: string) => void) => (value: string) => {
    if (create.isError) create.reset();
    set(value);
    if (!named) setName(nameFrom(value));
  };

  return (
    <Dialog
      open
      onClose={onClose}
      title="Add an app"
      // What happens next, said before it happens. Pando works out how to build
      // and run it, and shows you what it found before anything deploys
      // (R-102: ask, never guess).
      description="Pando works out how to build and run it, then shows you what it found. Nothing deploys until you accept."
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button
            variant="primary"
            disabled={create.isPending || source.trim() === '' || name.trim() === ''}
            onClick={() => create.mutate()}
          >
            {create.isPending ? 'Adding' : 'Add app'}
          </Button>
        </>
      }
    >
      <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-5)' }}>
        {kind === 'git' ? (
          <Input
            label="Repository"
            mono
            autoFocus
            value={url}
            placeholder="https://github.com/acme/notes"
            helper="Pando reads it to work out how to build and run the app. Nothing is read from it at deploy time."
            error={create.isError ? messageOf(create.error) : undefined}
            onChange={(e) => edit(setUrl)(e.target.value)}
          />
        ) : (
          <Input
            label="Image"
            mono
            autoFocus
            value={image}
            placeholder="nginx:1.27-alpine"
            helper="An image that is already built. Pando runs it as it is and skips working anything out."
            error={create.isError ? messageOf(create.error) : undefined}
            onChange={(e) => edit(setImage)(e.target.value)}
          />
        )}

        <Input
          label="Name"
          value={name}
          helper="What you will call it here. Taken from the source; change it if you like."
          onChange={(e) => {
            // Typing a name stops it following the source. Somebody who names
            // an app and then corrects the URL should not have their name
            // silently replaced.
            setNamed(true);
            setName(e.target.value);
          }}
        />

        {/* R-101's escape hatch: supply an image and skip detection. Second in
            the list and never the default, because the whole product is the
            first option working. */}
        <Select
          label="Where it comes from"
          value={kind}
          options={[
            { value: 'git', label: 'A Git repository' },
            { value: 'image', label: 'An image that is already built' },
          ]}
          onChange={(e) => {
            if (create.isError) create.reset();
            setKind(e.target.value as Kind);
          }}
        />

        {kind === 'image' && (
          <Banner tone="info">
            Pando won&rsquo;t work anything out for an image — you&rsquo;ll set the port and any other
            settings yourself.
          </Banner>
        )}
      </div>
    </Dialog>
  );
}

/** A name from a repository URL or an image reference. */
export function nameFrom(source: string): string {
  const trimmed = source.trim().replace(/\/+$/, '');
  if (trimmed === '') return '';

  // An image reference: drop the registry and the tag or digest.
  if (!trimmed.includes('://') && (trimmed.includes(':') || !trimmed.includes('.'))) {
    const last = trimmed.split('/').pop() ?? '';
    return (last.split('@')[0] ?? '').split(':')[0] ?? '';
  }

  const last = trimmed.split('/').pop() ?? '';
  return last.replace(/\.git$/, '');
}
