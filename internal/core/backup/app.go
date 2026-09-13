package backup

import (
	"archive/tar"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	"github.com/bemeek-io/pando/internal/adapter/api"
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

// AppRestoreRequest asks for an app's data to be put back.
type AppRestoreRequest struct {
	AppID      string
	AdapterRef string
	ObjectName string

	// Volumes are the app's volumes as they exist now, to restore into.
	Volumes []VolumeRef

	// Confirm is required. Restoring replaces the app's data with what was in
	// the backup, and anything written since is gone.
	Confirm bool
}

// AppRestoreResult reports what was put back.
type AppRestoreResult struct {
	Manifest       Manifest
	VolumesApplied int
}

// RestoreApp puts one app's data back, in place (R-206).
//
// In place is the whole of R-206: a backup restores to an app recreated from
// the same spec, matched by Pando's own identity for it. Backups are not
// portable to arbitrary apps, because Pando cannot know what is inside a volume
// and promising portable restore means promising semantics it cannot verify.
//
// Same order as the DR path and for the same reason: decrypt, verify, confirm,
// and only then write. A bundle that fails any of those leaves the app's data
// exactly as it was.
func (s *Service) RestoreApp(ctx context.Context, req AppRestoreRequest) (AppRestoreResult, error) {
	if !req.Confirm {
		return AppRestoreResult{}, errs.New(errs.ValidInvalid,
			"Restoring replaces this app's data with what was in the backup.").
			WithRemedy("Send confirm: true once you are sure. Anything written since the backup is lost.")
	}

	key, err := s.installKey()
	if err != nil {
		return AppRestoreResult{}, err
	}
	dest, _, err := s.destination(req.AdapterRef)
	if err != nil {
		return AppRestoreResult{}, err
	}
	if err := s.ensureWorkDir(); err != nil {
		return AppRestoreResult{}, err
	}

	rc, err := dest.Reader(ctx, req.ObjectName)
	if err != nil {
		return AppRestoreResult{}, err
	}
	defer func() { _ = rc.Close() }()

	staged, err := os.CreateTemp(s.WorkDir, "pando-app-restore-*.tar")
	if err != nil {
		return AppRestoreResult{}, errs.Wrap(errs.Internal, "Pando could not stage the restore.", err)
	}
	defer func() {
		_ = staged.Close()
		_ = os.Remove(staged.Name())
	}()

	if err := Decrypt(staged, rc, key); err != nil {
		if errors.Is(err, ErrPassphrase) {
			// Not a passphrase problem here — an app backup is unlocked with
			// the install's own key, so this means the key has changed or the
			// bundle is damaged. Said as what it is rather than as a passphrase
			// error the operator cannot act on.
			return AppRestoreResult{}, errs.New(errs.BackupDecryptFailed,
				"This backup cannot be opened with this installation's secrets key.").
				WithRemedy("It was taken by a different installation, or the key has been replaced since.")
		}
		return AppRestoreResult{}, err
	}
	if _, err := staged.Seek(0, io.SeekStart); err != nil {
		return AppRestoreResult{}, errs.Wrap(errs.Internal, "Pando could not read the staged restore.", err)
	}

	v, err := Verify(staged)
	if err != nil {
		return AppRestoreResult{}, err
	}
	if _, err := staged.Seek(0, io.SeekStart); err != nil {
		return AppRestoreResult{}, errs.Wrap(errs.Internal, "Pando could not read the staged restore.", err)
	}

	// Nothing has been touched until here.
	result := AppRestoreResult{Manifest: v.Manifest}
	byID := make(map[string]VolumeRef, len(req.Volumes))
	for _, vol := range req.Volumes {
		byID[vol.VolumeID] = vol
	}

	tr := tar.NewReader(staged)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return result, errs.Wrap(errs.Internal, "Pando could not read the backup while restoring.", err)
		}
		if header.Typeflag != tar.TypeReg || !strings.HasPrefix(header.Name, VolumesPrefix) {
			continue
		}

		volumeID := strings.TrimSuffix(strings.TrimPrefix(header.Name, VolumesPrefix), ".tar")
		target, known := byID[volumeID]
		if !known {
			// In the backup, not on the app any more — the volume was removed
			// from the spec since. Reported rather than dropped silently: the
			// data is real and somebody may want it.
			return result, errs.Newf(errs.StateInvalid,
				"This backup holds data for storage the app no longer has (%s).", volumeID).
				WithRemedy("Add that storage back to the app and restore again, or restore a newer backup.")
		}

		rt, ok := s.Registry.Runtime(target.AdapterRef)
		if !ok {
			return result, errs.Newf(errs.AdapterFailed,
				"The runtime holding %s is not configured.", volumeID)
		}
		if err := rt.RestoreVolume(ctx, api.VolumeHandle{
			VolumeID: target.VolumeID, Handle: target.Handle,
		}, tr); err != nil {
			return result, err
		}
		result.VolumesApplied++
	}
	return result, nil
}
