package backup

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"time"

	"github.com/bemeek-io/pando/internal/errs"
	"github.com/bemeek-io/pando/internal/secret"
)

// Per-app backups (R-204, R-205, R-210).
//
// Scope is "recover from a recent mistake", not disaster recovery (R-013). The
// one taken when an app is deleted is the important case: R-204 says Pando asks
// whether to keep a final copy, and that copy is kept until explicitly
// discarded rather than aged out.
//
// **Encrypted under the install's own secrets key, not a supplied passphrase.**
// That is a deliberate departure from R-213, which governs the DR bundle, and
// the reasoning there does not carry over: R-213 exists because a restore onto
// a *fresh machine* cannot unwrap keys held by the machine that died, and
// because shipping the key inside the encrypted bundle makes it plaintext for
// anyone holding the file. An app backup is restored in place, onto this
// install, by R-206 — so the machine that can read it is the machine that wrote
// it, and there is no key to ship anywhere.
//
// The alternative would be prompting for a passphrase every time someone
// deletes an app. That is a prompt people learn to type "password" into, which
// is worse than the key already protecting every secret in the install.

// AppBundleName is the fixed name of the spec inside a per-app bundle.
const AppSpecName = "spec.json"

// AppCreateRequest asks for a backup of one app.
type AppCreateRequest struct {
	AppID string

	// Kind is rolling or on_delete. An on_delete backup is kept until somebody
	// discards it (R-204), so it carries no retention at all.
	Kind string

	// Spec is the app's pinned spec, stored beside the data. Without it a
	// restore has bytes and no idea what ran them.
	Spec json.RawMessage

	// Volumes are the app's volumes, resolved to their runtime and handle.
	Volumes []VolumeRef

	DestinationRef string
	RetainFor      time.Duration
}

// CreateForApp backs up one app's data and spec.
func (s *Service) CreateForApp(ctx context.Context, id string, req AppCreateRequest) (Created, error) {
	key, err := s.installKey()
	if err != nil {
		return Created{}, err
	}

	dest, ref, err := s.destination(req.DestinationRef)
	if err != nil {
		return Created{}, err
	}
	if err := s.ensureWorkDir(); err != nil {
		return Created{}, err
	}

	staging, err := os.CreateTemp(s.WorkDir, "pando-app-backup-*.tar")
	if err != nil {
		return Created{}, errs.Wrap(errs.Internal, "Pando could not start the backup.", err)
	}
	defer func() {
		_ = staging.Close()
		_ = os.Remove(staging.Name())
	}()

	b := NewWriter(staging, req.Kind, s.Version, s.SchemaVersion)

	spec := req.Spec
	if len(spec) == 0 {
		spec = json.RawMessage("null")
	}
	if err := b.Add(AppSpecName, int64(len(spec)), bytesReader(spec)); err != nil {
		return Created{}, err
	}

	for _, v := range req.Volumes {
		if err := s.addVolume(ctx, b, v); err != nil {
			return Created{}, err
		}
	}
	b.Count("volumes", len(req.Volumes))

	manifest, err := b.Finish()
	if err != nil {
		return Created{}, err
	}
	if _, err := staging.Seek(0, io.SeekStart); err != nil {
		return Created{}, errs.Wrap(errs.Internal, "Pando could not read back the backup.", err)
	}

	w, err := dest.Writer(ctx, id)
	if err != nil {
		return Created{}, err
	}
	counter := &countingWriter{w: w}
	if err := Encrypt(counter, staging, key); err != nil {
		_ = w.Close()
		return Created{}, err
	}
	if err := w.Close(); err != nil {
		return Created{}, err
	}

	out := Created{ObjectName: id, AdapterRef: ref, SizeBytes: counter.n, Manifest: manifest}

	// R-204: an on_delete backup is kept until discarded, never aged out. The
	// schema refuses to give one an expiry, so this must not try.
	if req.Kind != "on_delete" && req.RetainFor > 0 && !dest.Capabilities().OwnsRetention {
		until := time.Now().UTC().Add(req.RetainFor)
		out.RetainUntil = &until
	}
	return out, nil
}

// installKey is the per-app backup key: the local secrets key, as a passphrase.
//
// Reusing Encrypt rather than inventing a second cipher, so per-app backups get
// the same construction the DR bundle does — the chunked AEAD with the
// last-chunk marker that makes truncation detectable. The only difference is
// where the key comes from.
func (s *Service) installKey() (secret.Value, error) {
	if s.SecretsKeyPath == "" {
		return secret.Value{}, errs.New(errs.Internal,
			"This installation has no local secrets key, so Pando cannot encrypt an app backup.").
			WithRemedy("Back up the whole installation instead, which is encrypted under a passphrase you supply.")
	}

	raw, err := os.ReadFile(s.SecretsKeyPath)
	if err != nil {
		return secret.Value{}, errs.Wrap(errs.Internal,
			"Pando could not read the key it encrypts app backups with.", err)
	}
	if len(raw) == 0 {
		return secret.Value{}, errs.New(errs.Internal,
			"This installation's secrets key is empty, so Pando cannot encrypt an app backup.")
	}
	return secret.New(base64.StdEncoding.EncodeToString(raw)), nil
}

// VerifyApp checks a per-app backup, which unlocks with the install's own key.
func (s *Service) VerifyApp(ctx context.Context, adapterRef, objectName string) (Verified, error) {
	key, err := s.installKey()
	if err != nil {
		return Verified{}, err
	}
	return s.Verify(ctx, adapterRef, objectName, key)
}
