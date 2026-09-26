//go:build integration

package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	adapterapi "github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/audit"
)

// blockingScanner is a scanner that holds each scan until the test lets it go.
type blockingScanner struct {
	started chan struct{}
	release chan struct{}
}

func (b *blockingScanner) Kind() string                                     { return "blocking" }
func (b *blockingScanner) Category() adapterapi.Category                    { return adapterapi.CategoryScanner }
func (b *blockingScanner) Configure(context.Context, json.RawMessage) error { return nil }
func (b *blockingScanner) HealthCheck(context.Context) error                { return nil }
func (b *blockingScanner) ScannerCapabilities() adapterapi.ScannerCapabilities {
	return adapterapi.ScannerCapabilities{ScansImages: true, ScansSource: true}
}

func (b *blockingScanner) Scan(ctx context.Context, _ adapterapi.ScanRequest) (adapterapi.ScanResult, error) {
	close(b.started)
	select {
	case <-b.release:
	case <-ctx.Done():
		return adapterapi.ScanResult{}, ctx.Err()
	}
	return adapterapi.ScanResult{Scanner: "blocking 1.0", Ran: time.Now().UTC()}, nil
}

// TestR310_AScanInProgressIsVisibleWhoeverStartedIt asserts that a scan the
// console did not start — a deploy's or detection's — shows as running on the
// app's security report and on its row in the app list, and stops showing when
// it ends (R-261: the state comes from the API, not from the client that asked).
func TestR310_AScanInProgressIsVisibleWhoeverStartedIt(t *testing.T) {
	i := newInstall(t)
	owner := i.admin()
	app := i.createApp(owner, "notes")

	scanner := &blockingScanner{started: make(chan struct{}), release: make(chan struct{})}
	require.NoError(t, i.Server.Registry.Register("scn_blocking", scanner))
	require.NoError(t, i.Server.Registry.SetDefault(adapterapi.CategoryScanner, "scn_blocking"))

	// Started the way a deploy starts one: through the service, by nobody the
	// console knows about.
	done := make(chan error, 1)
	go func() {
		_, err := i.Server.Security.Scan(context.Background(), adapterapi.ScanRequest{AppID: app},
			audit.Event{PrincipalKind: audit.KindSystem, PrincipalID: "test"})
		done <- err
	}()
	select {
	case <-scanner.started:
	case <-time.After(10 * time.Second):
		t.Fatal("the scan never reached the scanner")
	}

	type report struct {
		ScanningSince *time.Time `json:"scanning_since"`
		Scan          *struct {
			RanAt time.Time `json:"ran_at"`
		} `json:"scan"`
	}
	type list struct {
		Apps []struct {
			ID               string `json:"id"`
			SecurityScanning bool   `json:"security_scanning"`
		} `json:"apps"`
	}
	scanningInList := func() bool {
		got := i.do(owner, http.MethodGet, "/apps", nil)
		require.Equal(t, http.StatusOK, got.Code, got.String())
		var l list
		got.JSON(t, &l)
		for _, a := range l.Apps {
			if a.ID == app {
				return a.SecurityScanning
			}
		}
		t.Fatalf("app %s is not in the list", app)
		return false
	}

	got := i.do(owner, http.MethodGet, "/apps/"+app+"/security", nil)
	require.Equal(t, http.StatusOK, got.Code, got.String())
	var during report
	got.JSON(t, &during)
	require.NotNil(t, during.ScanningSince, "a running scan is reported: %s", got.String())
	require.Nil(t, during.Scan, "and it has not produced a scan yet")
	require.True(t, scanningInList(), "the app list shows it too")

	close(scanner.release)
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("the scan never finished")
	}

	got = i.do(owner, http.MethodGet, "/apps/"+app+"/security", nil)
	require.Equal(t, http.StatusOK, got.Code, got.String())
	var after report
	got.JSON(t, &after)
	require.Nil(t, after.ScanningSince, "a finished scan is not running: %s", got.String())
	require.False(t, scanningInList())
}
