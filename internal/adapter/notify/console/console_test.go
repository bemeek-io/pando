package console_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/api"
	notifyconsole "github.com/bemeek-io/pando/internal/adapter/notify/console"
	"github.com/bemeek-io/pando/internal/errs"
)

// recorded is one call to the sink.
type recorded struct {
	n      api.Notification
	retain time.Duration
}

// sink stands in for the writer main supplies. R-027 is why it is an interface:
// the adapter stores nothing itself.
type sink struct {
	calls []recorded
	err   error
}

func (s *sink) Record(_ context.Context, n api.Notification, retain time.Duration) error {
	s.calls = append(s.calls, recorded{n: n, retain: retain})
	return s.err
}

func configured(t *testing.T, raw json.RawMessage) (*notifyconsole.Adapter, *sink) {
	t.Helper()
	s := &sink{}
	a := notifyconsole.New(s)
	require.NoError(t, a.Configure(context.Background(), raw))
	return a, s
}

func TestIdentity(t *testing.T) {
	a := notifyconsole.New(&sink{})
	require.Equal(t, notifyconsole.Kind, a.Kind())
	require.Equal(t, api.CategoryNotify, a.Category())
}

// R-231: the notification is recorded and shown the next time someone looks.
// Nothing is sent anywhere, and that is the whole of it.
func TestR231_NotifyRecordsRatherThanSends(t *testing.T) {
	a, s := configured(t, nil)

	n := api.Notification{
		Recipients: []api.Recipient{{UserID: "usr_01HQ8"}},
	}
	require.NoError(t, a.Notify(context.Background(), n))

	require.Len(t, s.calls, 1)
	require.Equal(t, n.Recipients, s.calls[0].n.Recipients)
	require.Equal(t, 30*24*time.Hour, s.calls[0].retain, "the default retention")
}

func TestRetentionIsConfigurable(t *testing.T) {
	a, s := configured(t, json.RawMessage(`{"retain_days":7}`))
	require.NoError(t, a.Notify(context.Background(),
		api.Notification{Recipients: []api.Recipient{{UserID: "usr_01HQ8"}}}))

	require.Len(t, s.calls, 1)
	require.Equal(t, 7*24*time.Hour, s.calls[0].retain)
}

// A recipient Pando has no account for is someone this adapter cannot reach.
// Dropping them beats recording a notification addressed to nobody, which would
// look delivered.
func TestARecipientWithNoAccountIsDroppedNotGuessedAt(t *testing.T) {
	a, s := configured(t, nil)

	require.NoError(t, a.Notify(context.Background(), api.Notification{
		Recipients: []api.Recipient{
			{UserID: "usr_01HQ8"},
			{UserID: ""},
			{UserID: "usr_01HQ9"},
		},
	}))

	require.Len(t, s.calls, 1)
	require.Equal(t, []api.Recipient{{UserID: "usr_01HQ8"}, {UserID: "usr_01HQ9"}},
		s.calls[0].n.Recipients)
}

func TestANotificationNobodyCanReceiveIsNotRecorded(t *testing.T) {
	a, s := configured(t, nil)

	require.NoError(t, a.Notify(context.Background(), api.Notification{}))
	require.NoError(t, a.Notify(context.Background(), api.Notification{
		Recipients: []api.Recipient{{UserID: ""}},
	}))

	require.Empty(t, s.calls, "there is nobody to show it to")
}

func TestTheSinksErrorIsReported(t *testing.T) {
	s := &sink{err: errors.New("the notifications table is gone")}
	a := notifyconsole.New(s)
	require.NoError(t, a.Configure(context.Background(), nil))

	require.Error(t, a.Notify(context.Background(),
		api.Notification{Recipients: []api.Recipient{{UserID: "usr_01HQ8"}}}))
}

func TestConfigureRejectsAMalformedDocument(t *testing.T) {
	err := notifyconsole.New(&sink{}).Configure(context.Background(), json.RawMessage(`{`))
	require.Equal(t, errs.ValidInvalid, errs.CodeOf(err))
}

func TestAnAdapterWithNowhereToRecordIsUnhealthyAndRefusesToNotify(t *testing.T) {
	a := notifyconsole.New(nil)
	require.NoError(t, a.Configure(context.Background(), nil))

	require.Equal(t, errs.Internal, errs.CodeOf(a.HealthCheck(context.Background())))
	require.Equal(t, errs.Internal, errs.CodeOf(a.Notify(context.Background(),
		api.Notification{Recipients: []api.Recipient{{UserID: "usr_01HQ8"}}})))
}

func TestAnAdapterWithASinkIsHealthy(t *testing.T) {
	a, _ := configured(t, nil)
	require.NoError(t, a.HealthCheck(context.Background()))
}
