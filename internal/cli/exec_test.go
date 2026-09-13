package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

// execServer is the server side of design 04 §2.4: binary frames are the
// terminal, text frames are control.
func execServer(t *testing.T, handle func(ctx context.Context, conn *websocket.Conn, r *http.Request)) *Client {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok_test" {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]string{"message": "That token is not valid."},
			})
			return
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		handle(r.Context(), conn, r)
	}))
	t.Cleanup(srv.Close)

	return &Client{BaseURL: srv.URL + "/api/v1", Token: "tok_test", HTTP: srv.Client()}
}

func pipeFile(t *testing.T, content string) *os.File {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	go func() {
		_, _ = io.WriteString(w, content)
		_ = w.Close()
	}()
	t.Cleanup(func() { _ = r.Close() })
	return r
}

// TestR086_ExecCarriesTheTerminalBothWays asserts R-086 for the CLI.
//
// The API is the product (R-261): the CLI is a client of the same endpoint the
// console uses, and it must not need a second protocol to be usable.
func TestR086_ExecCarriesTheTerminalBothWays(t *testing.T) {
	var got string
	c := execServer(t, func(ctx context.Context, conn *websocket.Conn, _ *http.Request) {
		_, data, err := conn.Read(ctx)
		if err == nil {
			got = string(data)
		}
		_ = conn.Write(ctx, websocket.MessageBinary, []byte("hello from the container\n"))
		_ = conn.Close(websocket.StatusNormalClosure, "session ended")
	})

	var out strings.Builder
	err := c.Exec(context.Background(), ExecOptions{
		AppID: "app_01HQ8", Command: []string{"sh"},
		In: pipeFile(t, "whoami\n"), Out: &out, Err: io.Discard,
	})
	require.NoError(t, err)
	require.Equal(t, "hello from the container\n", out.String())
	require.Equal(t, "whoami\n", got, "keystrokes reach the container")
}

// The command is sent as repeated arguments, not one joined string.
//
// Joining on spaces would silently reinterpret `sh -c "a b"` as three arguments
// the first time someone passed a quoted string.
func TestTheCommandIsSentAsArgumentsNotAString(t *testing.T) {
	var query url.Values
	c := execServer(t, func(ctx context.Context, conn *websocket.Conn, r *http.Request) {
		query = r.URL.Query()
		_ = conn.Close(websocket.StatusNormalClosure, "session ended")
	})

	err := c.Exec(context.Background(), ExecOptions{
		AppID: "app_01HQ8", Workload: "web", Command: []string{"sh", "-c", "echo a b"},
		In: pipeFile(t, ""), Out: io.Discard, Err: io.Discard,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"sh", "-c", "echo a b"}, query["cmd"])
	require.Equal(t, "web", query.Get("workload"))
}

// A non-zero exit inside the container is the command's answer, carried out
// through the close reason, so `pando exec app -- false` behaves like `false`.
func TestANonZeroExitComesBackAsAStatus(t *testing.T) {
	c := execServer(t, func(ctx context.Context, conn *websocket.Conn, _ *http.Request) {
		_ = conn.Close(websocket.StatusNormalClosure, "exited with status 3")
	})

	err := c.Exec(context.Background(), ExecOptions{
		AppID: "app_01HQ8", In: pipeFile(t, ""), Out: io.Discard, Err: io.Discard,
	})
	require.Error(t, err)

	var exit *exitError
	require.ErrorAs(t, err, &exit)
	require.Equal(t, 3, exit.ExitCode())
}

// A clean exit is not an error, however the session ended.
func TestACleanExitIsNotAnError(t *testing.T) {
	c := execServer(t, func(ctx context.Context, conn *websocket.Conn, _ *http.Request) {
		_ = conn.Close(websocket.StatusNormalClosure, "session ended")
	})
	require.NoError(t, c.Exec(context.Background(), ExecOptions{
		AppID: "app_01HQ8", In: pipeFile(t, ""), Out: io.Discard, Err: io.Discard,
	}))
}

// A refusal keeps the server's own message.
//
// The handler refuses before upgrading — exec disabled by policy (R-085), the
// app never deployed, a workload the spec does not declare — so it arrives as
// an HTTP status with the error envelope in the body. Replacing that text with
// "connection failed" deletes the only useful thing in it (R-105).
func TestARefusedSessionKeepsTheServersMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{
			"message": "Opening a terminal is turned off on this installation.",
			"remedy":  "Ask an administrator about the installation's policy.",
		}})
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL + "/api/v1", Token: "tok_test", HTTP: srv.Client()}
	err := c.Exec(context.Background(), ExecOptions{
		AppID: "app_01HQ8", In: pipeFile(t, ""), Out: io.Discard, Err: io.Discard,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "turned off on this installation")
	require.Contains(t, err.Error(), "Ask an administrator")
}

// A close the session did not ask for is reported, not swallowed.
//
// A terminal that goes blank and exits 0 is indistinguishable from a command
// that finished, which is the wrong thing to tell someone mid-incident.
func TestAnAbruptCloseIsReported(t *testing.T) {
	c := execServer(t, func(_ context.Context, conn *websocket.Conn, _ *http.Request) {
		_ = conn.Close(websocket.StatusInternalError, "the runtime went away")
	})

	err := c.Exec(context.Background(), ExecOptions{
		AppID: "app_01HQ8", In: pipeFile(t, ""), Out: io.Discard, Err: io.Discard,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "the runtime went away")
}

// https becomes wss. Dialing ws:// against an https install would either fail
// or, worse, downgrade a terminal carrying secrets onto a plaintext socket.
func TestTheSchemeFollowsTheBaseURL(t *testing.T) {
	for base, want := range map[string]string{
		"https://pando.example.com/api/v1": "wss://",
		"http://localhost:8080/api/v1":     "ws://",
	} {
		got, err := (&Client{BaseURL: base}).execURL(ExecOptions{AppID: "app_01HQ8"})
		require.NoError(t, err)
		require.True(t, strings.HasPrefix(got, want), "%s should dial %s, got %s", base, want, got)
	}
}

// Large output arrives in one frame after a `cat` of something big. The default
// read limit would close the session as a protocol violation instead.
func TestALargeBurstOfOutputDoesNotKillTheSession(t *testing.T) {
	big := strings.Repeat("x", 1<<20)
	c := execServer(t, func(ctx context.Context, conn *websocket.Conn, _ *http.Request) {
		_ = conn.Write(ctx, websocket.MessageBinary, []byte(big))
		_ = conn.Close(websocket.StatusNormalClosure, "session ended")
	})

	var out strings.Builder
	err := c.Exec(context.Background(), ExecOptions{
		AppID: "app_01HQ8", In: pipeFile(t, ""), Out: &out, Err: io.Discard,
	})
	require.NoError(t, err)
	require.Len(t, out.String(), len(big))
}

// A cancelled context ends the session rather than hanging.
func TestCancellingEndsTheSession(t *testing.T) {
	c := execServer(t, func(ctx context.Context, _ *websocket.Conn, _ *http.Request) {
		<-ctx.Done()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- c.Exec(ctx, ExecOptions{
			AppID: "app_01HQ8", In: pipeFile(t, ""), Out: io.Discard, Err: io.Discard,
		})
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the session did not end when its context was cancelled")
	}
}
