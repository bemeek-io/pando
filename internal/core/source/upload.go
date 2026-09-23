package source

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/errs"
)

// Uploaded source (R-262).
//
// `pando deploy ./` is a [D] in design 04 §4, and the reason is specific: an
// agent that just generated an app cannot commit it and push, but it can run a
// command. Without this, the agent workflow R-262 describes starts by asking a
// person to make a repository.
//
// An upload is stored as a gzipped tar under a directory Pando owns, keyed by
// the app. It is the source of record for that app's next deploy, so it is kept
// rather than streamed: R-020 says nothing is read from the repository at
// deploy time, and the same logic applies here — the deploy reads what was
// uploaded, not whatever is on somebody's laptop now.

// UploadDir is where uploaded sources are kept. Set at startup.
var UploadDir = "/var/lib/pando/uploads"

// StoreUpload writes an uploaded archive for an app and returns its path.
func StoreUpload(appID string, r io.Reader) (string, error) {
	if err := os.MkdirAll(UploadDir, 0o700); err != nil {
		return "", errs.Wrap(errs.Internal, "Pando could not store the upload.", err)
	}

	// Written to a temporary name and renamed, so a deploy that runs while an
	// upload is in flight reads the previous archive rather than half of the
	// new one.
	final := filepath.Join(UploadDir, appID+".tar.gz")
	tmp := final + ".partial"

	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", errs.Wrap(errs.Internal, "Pando could not store the upload.", err)
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return "", errs.Wrap(errs.Internal, "Pando could not store the upload.", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return "", errs.Wrap(errs.Internal, "Pando could not store the upload.", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return "", errs.Wrap(errs.Internal, "Pando could not store the upload.", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return "", errs.Wrap(errs.Internal, "Pando could not store the upload.", err)
	}
	return final, nil
}

// fetchUpload expands a stored upload into a checkout.
func fetchUpload(_ context.Context, src spec.Source) (*Checkout, error) {
	archive := filepath.Join(UploadDir, src.UploadID+".tar.gz")
	f, err := os.Open(archive)
	if errors.Is(err, os.ErrNotExist) {
		return nil, errs.New(errs.ValidInvalid,
			"This app's uploaded source is no longer on the server.").
			WithRemedy("Run `pando deploy .` from the app's directory again.")
	}
	if err != nil {
		return nil, errs.Wrap(errs.Internal, "Pando could not read the uploaded source.", err)
	}
	defer func() { _ = f.Close() }()

	dir, err := os.MkdirTemp("", "pando-upload-")
	if err != nil {
		return nil, errs.Wrap(errs.Internal, "Pando could not unpack the uploaded source.", err)
	}

	if err := extract(f, dir); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	// No commit: an upload has no revision. The deploy records the archive it
	// came from instead, which is the honest answer to "what was deployed".
	//
	// With the same cleanup a clone has. Without it every detection and every
	// deploy of an uploaded app left a full copy of its source in the
	// temporary directory for as long as the server ran (issue #55).
	return &Checkout{Dir: dir, Commit: "", cleanup: func() { _ = os.RemoveAll(dir) }}, nil
}

// extract unpacks a gzipped tar, refusing anything that escapes the directory.
//
// Path traversal in a tar is the oldest trick there is, and this archive comes
// from whatever a user or an agent chose to send. The check is on the resolved
// path rather than the name, because ../ is not the only way to leave a
// directory — a symlink pointing out of it and a later entry written through
// that symlink is the other, which is why symlinks are dropped entirely.
func extract(r io.Reader, dir string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return errs.Wrap(errs.ValidInvalid, "That upload is not a gzipped tar archive.", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return errs.Wrap(errs.ValidInvalid, "That upload could not be read.", err)
		}

		target := filepath.Join(dir, filepath.Clean("/"+header.Name))
		if !strings.HasPrefix(target, filepath.Clean(dir)+string(os.PathSeparator)) && target != dir {
			return errs.Newf(errs.ValidInvalid,
				"That upload contains a path that would write outside the app's directory: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return errs.Wrap(errs.Internal, "Pando could not unpack the upload.", err)
			}

		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return errs.Wrap(errs.Internal, "Pando could not unpack the upload.", err)
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fileMode(header.Mode))
			if err != nil {
				return errs.Wrap(errs.Internal, "Pando could not unpack the upload.", err)
			}
			// Bounded: an archive claiming a petabyte should fail on the limit
			// rather than on the disk.
			if _, err := io.Copy(out, io.LimitReader(tr, maxUploadFileBytes)); err != nil {
				_ = out.Close()
				return errs.Wrap(errs.Internal, "Pando could not unpack the upload.", err)
			}
			if err := out.Close(); err != nil {
				return errs.Wrap(errs.Internal, "Pando could not unpack the upload.", err)
			}

		default:
			// Symlinks, devices, fifos: skipped, not an error. A source tree
			// with a symlink in it is normal and should still deploy; a symlink
			// that Pando followed while unpacking is how an archive writes
			// outside its directory.
		}
	}
}

const maxUploadFileBytes = 1 << 30 // 1 GiB per file

// fileMode takes the archive's permissions, within limits.
//
// Honoring the mode matters more than it looks: extracting everything 0600
// produces a source tree only root can read, and a build that then runs as a
// different user — nginx serving static files, say — answers 403 for every file
// in the app. That failure appears at runtime, in the app, looking like the
// app's fault.
//
// Only the permission bits, so setuid and setgid cannot arrive in an upload.
// An executable bit is kept, because a repository with a build script in it
// needs one.
func fileMode(mode int64) os.FileMode {
	// G115: Perm masks to the low nine bits after the conversion, so a mode
	// that overflows uint32 cannot produce a permission this function did not
	// intend. The mask is also what drops setuid and setgid.
	perm := os.FileMode(mode).Perm() //nolint:gosec
	if perm == 0 {
		return 0o644
	}
	// Always readable and writable by the owner: Pando has to be able to read
	// back what it just wrote, whatever the archive claimed.
	return perm | 0o600
}
