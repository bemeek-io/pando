//go:build integration

package acceptance_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Sequence D — disaster recovery.
//
// The flow nobody tests until they need it, which is why it is one of the four.
// Phase 9's Done when: Sequence D passes, including rejection of a tampered
// bundle **with the target untouched**. Not mostly untouched.

const bundlePassphrase = "a-long-backup-passphrase-for-tests"

func takeBackup(t *testing.T, c *client) string {
	t.Helper()
	body, status := c.do(t, http.MethodPost, "/backups",
		fmt.Sprintf(`{"passphrase":%q}`, bundlePassphrase))
	require.Equal(t, http.StatusCreated, status, body)

	var rec map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &rec))
	return rec["id"].(string)
}

// TestR212_ADRBundleContainsWhatItPromises asserts the bundle is the whole
// install, not a database dump with a hopeful name.
//
// A bundle missing the secrets key restores an install holding every app's
// ciphertext and nothing that opens it — which looks like success right up
// until an app starts.
func TestR212_ADRBundleContainsWhatItPromises(t *testing.T) {
	admin := login(t)
	id := takeBackup(t, admin)

	body, status := admin.do(t, http.MethodPost, "/backups/"+id+"/verify",
		fmt.Sprintf(`{"passphrase":%q}`, bundlePassphrase))
	require.Equal(t, http.StatusOK, status, body)

	var out struct {
		Verified bool `json:"verified"`
		Manifest struct {
			Entries []struct {
				Name   string `json:"name"`
				SHA256 string `json:"sha256"`
			} `json:"entries"`
			Counts        map[string]int `json:"counts"`
			SchemaVersion int            `json:"schema_version"`
		} `json:"manifest"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &out))
	require.True(t, out.Verified)

	names := map[string]bool{}
	for _, e := range out.Manifest.Entries {
		names[e.Name] = true
		require.Len(t, e.SHA256, 64, "every entry carries a checksum, or verification proves nothing")
	}
	require.True(t, names["postgres.dump"], "the state store")
	require.True(t, names["secrets.key"], "R-212: without this a restore cannot read any app's secrets")
	require.True(t, names["adapters.json"])
	require.True(t, names["policy.json"])

	// Counts, so an operator at a restore prompt can tell whether this is the
	// install they think it is. Checksums only prove the bytes arrived.
	require.Positive(t, out.Manifest.Counts["users"])
	require.Positive(t, out.Manifest.SchemaVersion)
}

// TestR214_AWrongPassphraseRevealsNothing asserts the accepted cost of R-213.
//
// It must also fail *as a passphrase problem*. Reporting it as a damaged bundle
// sends an operator to hunt for an older backup when the one in their hand is
// perfectly good — which, in a disaster, is the difference between a bad hour
// and a bad week.
func TestR214_AWrongPassphraseRevealsNothing(t *testing.T) {
	admin := login(t)
	id := takeBackup(t, admin)

	body, status := admin.do(t, http.MethodPost, "/backups/"+id+"/verify",
		`{"passphrase":"not the passphrase"}`)
	require.Equal(t, http.StatusUnprocessableEntity, status, body)
	require.Contains(t, body, "BACKUP_DECRYPT_FAILED")

	// Nothing about the contents leaks — no entry names, no counts, no sizes.
	require.NotContains(t, body, "postgres.dump")
	require.NotContains(t, body, "secrets.key")
	require.NotContains(t, body, "manifest")
}

// TestR215_RestoreRefusesWithoutConfirmation asserts Sequence D step 3.
//
// Restoring replaces every app, account and secret. A caller who has not said
// so explicitly is a caller who meant to verify.
func TestR215_RestoreRefusesWithoutConfirmation(t *testing.T) {
	admin := login(t)
	id := takeBackup(t, admin)

	// A witness created after the backup. If the restore ran, it would be gone.
	witness := uniqueName("witness")
	admin.createUser(t, witness)

	body, status := admin.do(t, http.MethodPost, "/backups/"+id+"/restore",
		fmt.Sprintf(`{"passphrase":%q}`, bundlePassphrase))
	require.Equal(t, http.StatusBadRequest, status, body)
	require.Contains(t, body, "confirm")

	require.True(t, userExists(t, admin, witness),
		"an unconfirmed restore must not touch the installation")
}

// TestR215_AWrongPassphraseLeavesTheInstallUntouched is the first half of the
// Done when: rejected, with the target untouched.
func TestR215_AWrongPassphraseLeavesTheInstallUntouched(t *testing.T) {
	admin := login(t)
	id := takeBackup(t, admin)

	witness := uniqueName("witness")
	admin.createUser(t, witness)

	body, status := admin.do(t, http.MethodPost, "/backups/"+id+"/restore",
		`{"passphrase":"not the passphrase","confirm":true}`)
	require.Equal(t, http.StatusUnprocessableEntity, status, body)
	require.Contains(t, body, "BACKUP_DECRYPT_FAILED")

	// Confirmed, and still nothing happened: decryption is step 1 and the
	// install is not touched until step 4.
	require.True(t, userExists(t, admin, witness),
		"a restore that failed to decrypt must leave the installation exactly as it was")
}

// TestR215_ATamperedBundleIsRejectedWithTheTargetUntouched is the assertion
// phase 9's Done when names explicitly.
//
// The bundle is damaged at the destination — the way a real one is, by the
// storage under it — and the restore must refuse at verification rather than
// discover the problem while applying.
func TestR215_ATamperedBundleIsRejectedWithTheTargetUntouched(t *testing.T) {
	admin := login(t)
	id := takeBackup(t, admin)

	witness := uniqueName("witness")
	admin.createUser(t, witness)

	corruptStoredBundle(t, id)

	body, status := admin.do(t, http.MethodPost, "/backups/"+id+"/restore",
		fmt.Sprintf(`{"passphrase":%q,"confirm":true}`, bundlePassphrase))
	require.Equal(t, http.StatusUnprocessableEntity, status, body)

	require.True(t, userExists(t, admin, witness),
		"a tampered bundle must be rejected with the target untouched — not mostly untouched")

	// And verify says so too, so an operator can find this out before the
	// disaster rather than during it (R-216).
	body, status = admin.do(t, http.MethodPost, "/backups/"+id+"/verify",
		fmt.Sprintf(`{"passphrase":%q}`, bundlePassphrase))
	require.NotEqual(t, http.StatusOK, status, body)
}

// TestR212_RestoreReturnsTheInstallToItsBackedUpState is the happy path, and
// the one that matters: the bundle has to actually work.
func TestR212_RestoreReturnsTheInstallToItsBackedUpState(t *testing.T) {
	admin := login(t)

	// Present at backup time.
	before := uniqueName("before")
	admin.createUser(t, before)

	id := takeBackup(t, admin)

	// Created afterwards, so it must not survive.
	after := uniqueName("after")
	admin.createUser(t, after)
	require.True(t, userExists(t, admin, after))

	body, status := admin.do(t, http.MethodPost, "/backups/"+id+"/restore",
		fmt.Sprintf(`{"passphrase":%q,"confirm":true}`, bundlePassphrase))
	require.Equal(t, http.StatusOK, status, body)

	require.True(t, userExists(t, admin, before), "an account present at backup time must come back")
	require.False(t, userExists(t, admin, after), "an account created after the backup must not survive it")

	// The bundle survives being used. It contains no record of itself — a
	// backup cannot — so without the handler putting the row back, restoring
	// once would make the bundle unlistable and unrestorable.
	body, status = admin.do(t, http.MethodPost, "/backups/"+id+"/restore",
		fmt.Sprintf(`{"passphrase":%q,"confirm":true}`, bundlePassphrase))
	require.Equal(t, http.StatusOK, status, body)
}

// TestR227_TheRestoreIsRecordedInTheInstallItProduced asserts the audit trail
// survives into the restored install.
//
// It has to land in the restored database rather than the one being replaced,
// or the only record of the most destructive action in the system is in the
// state that action threw away.
func TestR227_TheRestoreIsRecordedInTheInstallItProduced(t *testing.T) {
	admin := login(t)
	id := takeBackup(t, admin)

	body, status := admin.do(t, http.MethodPost, "/backups/"+id+"/restore",
		fmt.Sprintf(`{"passphrase":%q,"confirm":true}`, bundlePassphrase))
	require.Equal(t, http.StatusOK, status, body)

	log := admin.get(t, "/audit?action=backup.restore")
	events, ok := log["events"].([]any)
	require.True(t, ok, "%v", log)
	require.NotEmpty(t, events, "the restore must be recorded in the installation it produced")
}

// TestR080_BackupsRequireTheirOwnVerb asserts install.backup.manage gates all
// four routes.
//
// Its own verb rather than folded into policy management: a restore replaces
// the whole install, and handing that to everyone who can edit a source
// allowlist is not the same trust.
func TestR080_BackupsRequireTheirOwnVerb(t *testing.T) {
	admin := login(t)
	outsider := admin.asUser(t, admin.createUser(t, uniqueName("kim")))
	id := takeBackup(t, admin)

	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/backups", ""},
		{http.MethodPost, "/backups", fmt.Sprintf(`{"passphrase":%q}`, bundlePassphrase)},
		{http.MethodPost, "/backups/" + id + "/verify", fmt.Sprintf(`{"passphrase":%q}`, bundlePassphrase)},
		{http.MethodPost, "/backups/" + id + "/restore", fmt.Sprintf(`{"passphrase":%q,"confirm":true}`, bundlePassphrase)},
	} {
		body, status := outsider.do(t, tc.method, tc.path, tc.body)
		require.Equal(t, http.StatusForbidden, status, "%s %s: %s", tc.method, tc.path, body)
		require.Contains(t, body, "install.backup.manage")
	}
}

// TestR213_AShortPassphraseIsRefused — the only rule, and it is length.
//
// No composition requirements: they produce shorter, more guessable secrets and
// a note on a monitor. This one guards every secret in the installation and
// there is no recovery path, so the floor is higher than a password's.
func TestR213_AShortPassphraseIsRefused(t *testing.T) {
	admin := login(t)

	body, status := admin.do(t, http.MethodPost, "/backups", `{"passphrase":"short"}`)
	require.Equal(t, http.StatusBadRequest, status, body)
	require.Contains(t, body, "16 characters")
	require.Contains(t, body, "never stores", "the message has to say the passphrase is unrecoverable")
}

// --- helpers ---------------------------------------------------------------

func userExists(t *testing.T, c *client, username string) bool {
	t.Helper()
	body, status := c.do(t, http.MethodGet, "/users", "")
	require.Equal(t, http.StatusOK, status, body)

	var out struct {
		Users []struct {
			ExternalID string `json:"external_id"`
		} `json:"users"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &out))
	for _, u := range out.Users {
		if u.ExternalID == username {
			return true
		}
	}
	return false
}

// corruptStoredBundle flips bytes in the middle of a bundle at its destination.
//
// In the middle rather than at the end, so this is not the truncation case in
// the unit tests: a bundle whose length is right and whose contents are not is
// what silent storage corruption actually looks like.
func corruptStoredBundle(t *testing.T, id string) {
	t.Helper()

	// The bundle lives in the pando container's local destination.
	out, err := composeExec("pando", "sh", "-c",
		fmt.Sprintf("f=/var/lib/pando/backups/%s; "+
			"size=$(wc -c < $f); "+
			"dd if=/dev/urandom of=$f bs=1 seek=$((size/2)) count=64 conv=notrunc 2>/dev/null && echo ok", id))
	require.NoError(t, err, "could not damage the stored bundle: %s", out)
	require.Contains(t, out, "ok", out)
}

func composeExec(service string, args ...string) (string, error) {
	full := append([]string{"compose", "exec", "-T", service}, args...)
	out, err := exec.Command("docker", full...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
