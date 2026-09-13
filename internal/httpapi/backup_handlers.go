package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/bemeek-io/pando/internal/core/audit"
	"github.com/bemeek-io/pando/internal/core/authz"
	"github.com/bemeek-io/pando/internal/core/backup"
	"github.com/bemeek-io/pando/internal/core/state"
	"github.com/bemeek-io/pando/internal/errs"
	"github.com/bemeek-io/pando/internal/secret"
)

// Backup endpoints (design 04 §2.8, Sequence D).
//
// All four behind install.backup.manage. Listing is included rather than given
// install.view, because the list names what exists to be restored and when the
// install was last protected — which is reconnaissance for anyone deciding
// whether it is worth attacking.

func (s *Server) handleListBackups(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireInstall(w, r, authz.InstallBackupManage); !ok {
		return
	}
	if s.Backups == nil {
		Error(w, r, errs.New(errs.Internal, "Backups are not set up on this installation."))
		return
	}

	rows, err := s.Backups.List(r.Context(), r.URL.Query().Get("app_id"))
	if err != nil {
		Error(w, r, err)
		return
	}
	JSON(w, http.StatusOK, map[string]any{"backups": rows})
}

type createBackupRequest struct {
	// Passphrase is never stored (R-213). Losing it makes the bundle unusable
	// (R-214) — the console says so at the point of creation, which is the only
	// place saying it does any good.
	Passphrase string `json:"passphrase"`

	DestinationRef string `json:"destination_ref"`

	// RetainDays of 0 means keep until explicitly discarded.
	RetainDays int `json:"retain_days"`
}

// handleCreateBackup takes a full-host DR bundle (R-212).
//
// Synchronous, and deliberately: a backup that returns 202 and fails in the
// background is a backup an operator believes they have. The cost is a long
// request, which is the right cost for this — nobody takes a DR bundle in a
// loop.
func (s *Server) handleCreateBackup(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requireInstall(w, r, authz.InstallBackupManage)
	if !ok {
		return
	}
	if s.Backups == nil || s.Backup == nil {
		Error(w, r, errs.New(errs.Internal, "Backups are not set up on this installation."))
		return
	}

	var req createBackupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, r, errs.New(errs.ValidInvalid, "The request body could not be read."))
		return
	}
	if len(req.Passphrase) < minPassphraseLength {
		Error(w, r, errs.Newf(errs.ValidInvalid,
			"A backup passphrase needs at least %d characters.", minPassphraseLength).
			WithRemedy("Pando never stores this passphrase, so choose something you can find again after the machine is gone."))
		return
	}

	id := s.Backups.NewID()
	created, err := s.Backup.Create(r.Context(), id, backup.CreateRequest{
		Passphrase:     secret.New(req.Passphrase),
		DestinationRef: req.DestinationRef,
		RetainFor:      time.Duration(req.RetainDays) * 24 * time.Hour,
	})
	if err != nil {
		s.audit(r, audit.Event{
			PrincipalKind: audit.PrincipalKind(p.Kind), PrincipalID: p.ID, OnBehalfOf: p.UserID,
			Action: "backup.failed", TargetKind: "backup", TargetID: id,
		})
		Error(w, r, err)
		return
	}

	// Recorded only once the object is written. A row written first would claim
	// a bundle exists during the window where it does not.
	rec := state.Backup{
		ID: id, Kind: "dr_bundle", AdapterRef: created.AdapterRef, ObjectName: created.ObjectName,
		SizeBytes: created.SizeBytes, Manifest: created.Manifest,
		RetainUntil: created.RetainUntil, CreatedBy: p.ID,
	}
	if err := s.Backups.Record(r.Context(), rec); err != nil {
		Error(w, r, err)
		return
	}

	s.audit(r, audit.Event{
		PrincipalKind: audit.PrincipalKind(p.Kind), PrincipalID: p.ID, OnBehalfOf: p.UserID,
		Action: "backup.create", TargetKind: "backup", TargetID: id,
		Detail: map[string]any{
			"destination": created.AdapterRef,
			"size_bytes":  created.SizeBytes,
			"counts":      created.Manifest.Counts,
		},
	})
	JSON(w, http.StatusCreated, rec)
}

// handleVerifyBackup checks a bundle without applying it (R-216).
//
// Its own route rather than a flag on restore. A flag is a thing somebody
// passes wrongly, and the wrong value here replaces an installation.
func (s *Server) handleVerifyBackup(w http.ResponseWriter, r *http.Request) {
	p, rec, ok := s.requireBackup(w, r)
	if !ok {
		return
	}

	var req struct {
		Passphrase string `json:"passphrase"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, r, errs.New(errs.ValidInvalid, "The request body could not be read."))
		return
	}

	v, err := s.Backup.Verify(r.Context(), rec.AdapterRef, rec.ObjectName, secret.New(req.Passphrase))
	if err != nil {
		s.audit(r, audit.Event{
			PrincipalKind: audit.PrincipalKind(p.Kind), PrincipalID: p.ID, OnBehalfOf: p.UserID,
			Action: "backup.verify.failed", TargetKind: "backup", TargetID: rec.ID,
			Detail: map[string]any{"code": string(errs.CodeOf(err))},
		})
		Error(w, r, err)
		return
	}

	s.audit(r, audit.Event{
		PrincipalKind: audit.PrincipalKind(p.Kind), PrincipalID: p.ID, OnBehalfOf: p.UserID,
		Action: "backup.verify", TargetKind: "backup", TargetID: rec.ID,
	})
	JSON(w, http.StatusOK, map[string]any{
		"verified": true,
		"manifest": v.Manifest,
	})
}

// handleRestoreBackup replaces this installation from a bundle.
//
// The most destructive action the API has. Verification happens first and the
// target is untouched if it fails (R-215); confirmation is explicit; and the
// audit event is written *before* the restore, because a restore that
// half-succeeds must still be recorded as attempted — the same ordering exec
// uses for the same reason.
func (s *Server) handleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	p, rec, ok := s.requireBackup(w, r)
	if !ok {
		return
	}

	var req struct {
		Passphrase string `json:"passphrase"`
		Confirm    bool   `json:"confirm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, r, errs.New(errs.ValidInvalid, "The request body could not be read."))
		return
	}

	s.audit(r, audit.Event{
		PrincipalKind: audit.PrincipalKind(p.Kind), PrincipalID: p.ID, OnBehalfOf: p.UserID,
		Action: "backup.restore.start", TargetKind: "backup", TargetID: rec.ID,
	})

	result, err := s.Backup.Restore(r.Context(), backup.RestoreRequest{
		AdapterRef: rec.AdapterRef, ObjectName: rec.ObjectName,
		Passphrase: secret.New(req.Passphrase), Confirm: req.Confirm,
	})
	if err != nil {
		s.audit(r, audit.Event{
			PrincipalKind: audit.PrincipalKind(p.Kind), PrincipalID: p.ID, OnBehalfOf: p.UserID,
			Action: "backup.restore.refused", TargetKind: "backup", TargetID: rec.ID,
			Detail: map[string]any{"code": string(errs.CodeOf(err))},
		})
		Error(w, r, err)
		return
	}

	// Put the bundle's own row back.
	//
	// It was not there when the bundle was taken — a backup cannot contain the
	// record of itself — so the restore just erased it, and without this the
	// bundle an operator is standing on becomes unlistable and unrestorable the
	// moment they use it once. Found by restoring twice.
	if err := s.Backups.Record(r.Context(), rec); err != nil {
		// Reported, not fatal. The install is restored; this is bookkeeping,
		// and failing the request here would say the restore failed when it
		// did not.
		s.Logger.Warn("restored install has no record of the bundle it came from",
			zap.String("backup_id", rec.ID), zap.Error(err))
	}

	// This event lands in the restored database, which is the point: the
	// restore is part of the history of the install it produced.
	s.audit(r, audit.Event{
		PrincipalKind: audit.PrincipalKind(p.Kind), PrincipalID: p.ID, OnBehalfOf: p.UserID,
		Action: "backup.restore", TargetKind: "backup", TargetID: rec.ID,
		Detail: map[string]any{"volumes": result.VolumesApplied},
	})
	JSON(w, http.StatusOK, map[string]any{
		"restored": true,
		"volumes":  result.VolumesApplied,
		"manifest": result.Manifest,
	})
}

// handleRestoreAppBackup puts one app's data back (R-206, R-210).
//
// Distinct from restoring a DR bundle, which replaces the installation. This
// replaces one app's data, in place, and is gated on the app rather than the
// install: it is an app operation, so an app's owner can do it without holding
// install.backup.manage.
func (s *Server) handleRestoreAppBackup(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireControl(w, r, authz.AppDeploy)
	if !ok {
		return
	}
	if s.Backups == nil || s.Backup == nil || s.BundleSource == nil {
		Error(w, r, errs.New(errs.Internal, "Backups are not set up on this installation."))
		return
	}

	var req struct {
		BackupID string `json:"backup_id"`
		Confirm  bool   `json:"confirm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, r, errs.New(errs.ValidInvalid, "The request body could not be read."))
		return
	}

	rec, found, err := s.Backups.ByID(r.Context(), req.BackupID)
	if err != nil {
		Error(w, r, err)
		return
	}
	if !found {
		Error(w, r, errs.New(errs.NotFound, "There is no backup with that ID."))
		return
	}
	if rec.AppID != app.ID {
		// R-206: in place only, matched by Pando's own identity for the app.
		// Restoring one app's data into another is a promise Pando cannot keep
		// — it cannot know what is inside a volume — so it is refused rather
		// than attempted.
		Error(w, r, errs.New(errs.ValidInvalid, "That backup belongs to a different app.").
			WithRemedy("A backup restores only to the app it came from."))
		return
	}

	volumes, err := s.BundleSource.VolumesForApp(r.Context(), app.ID)
	if err != nil {
		Error(w, r, err)
		return
	}

	p := PrincipalFrom(r.Context())
	s.audit(r, audit.Event{
		PrincipalKind: audit.PrincipalKind(p.Kind), PrincipalID: p.ID, OnBehalfOf: p.UserID,
		Action: "app.restore.start", AppID: app.ID, TargetKind: "backup", TargetID: rec.ID,
	})

	result, err := s.Backup.RestoreApp(r.Context(), backup.AppRestoreRequest{
		AppID: app.ID, AdapterRef: rec.AdapterRef, ObjectName: rec.ObjectName,
		Volumes: volumes, Confirm: req.Confirm,
	})
	if err != nil {
		Error(w, r, err)
		return
	}

	s.audit(r, audit.Event{
		PrincipalKind: audit.PrincipalKind(p.Kind), PrincipalID: p.ID, OnBehalfOf: p.UserID,
		Action: "app.restore", AppID: app.ID, TargetKind: "backup", TargetID: rec.ID,
		Detail: map[string]any{"volumes": result.VolumesApplied},
	})
	JSON(w, http.StatusOK, map[string]any{
		"restored": true, "volumes": result.VolumesApplied,
		"note": "Restart the app so it reads the restored data.",
	})
}

// requireBackup resolves the backup and checks the verb.
func (s *Server) requireBackup(w http.ResponseWriter, r *http.Request) (authz.Principal, state.Backup, bool) {
	p, ok := s.requireInstall(w, r, authz.InstallBackupManage)
	if !ok {
		return p, state.Backup{}, false
	}
	if s.Backups == nil || s.Backup == nil {
		Error(w, r, errs.New(errs.Internal, "Backups are not set up on this installation."))
		return p, state.Backup{}, false
	}

	rec, found, err := s.Backups.ByID(r.Context(), chi.URLParam(r, "backupID"))
	if err != nil {
		Error(w, r, err)
		return p, state.Backup{}, false
	}
	if !found {
		Error(w, r, errs.New(errs.NotFound, "There is no backup with that ID."))
		return p, state.Backup{}, false
	}
	return p, rec, true
}

// minPassphraseLength is the only rule, and it is longer than a password's.
//
// A password protects an account somebody can regain by other means; this
// protects every secret in the installation and there is no other means. No
// composition requirements, for the same reason as elsewhere: they produce
// shorter, more guessable secrets and a note on a monitor.
const minPassphraseLength = 16
