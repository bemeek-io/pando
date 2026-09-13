package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/coder/websocket"
	"golang.org/x/term"
)

// ExecOptions is one terminal session.
type ExecOptions struct {
	AppID    string
	Workload string
	Command  []string

	// In, Out and Err are the local ends of the terminal. Fields rather than
	// os.Stdin directly so the failure path can be tested without a tty.
	In  *os.File
	Out io.Writer
	Err io.Writer
}

// control is the resize message, matching the server's (design 04 §2.4).
//
// Sent as a text frame while the terminal's bytes are binary frames. The split
// is why a resize is not something a program can produce by writing to stdout:
// a byte stream carrying its own escape-coded control messages is a byte stream
// an app inside the container can forge.
type control struct {
	Rows uint16 `json:"rows"`
	Cols uint16 `json:"cols"`
}

// Exec opens a terminal inside a running workload (R-086).
//
// Returns the command's exit status so `pando exec app -- false` can be used in
// a script the way ssh is. A transport failure is a Go error; a non-zero exit
// inside the container is an exitError and not a failure of this command.
func (c *Client) Exec(ctx context.Context, opts ExecOptions) error {
	endpoint, err := c.execURL(opts)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{
		HTTPClient: c.HTTP,
		HTTPHeader: http.Header{"Authorization": {"Bearer " + c.Token}},
	})
	// Closed on both paths. On success the library has already drained and
	// closed it — the body of a 101 is the socket — but a nil check here costs
	// nothing and a leaked connection on the failure path costs a file
	// descriptor per refused session.
	if resp != nil && resp.Body != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		return execDialError(err, resp)
	}
	defer func() { _ = conn.CloseNow() }()

	// A terminal's worth of output arrives in one frame after a `cat` of
	// something large. The default read limit is 32KiB, which would close the
	// session as a protocol violation.
	conn.SetReadLimit(8 << 20)

	// Raw mode, so keystrokes reach the container rather than the local line
	// editor: without it Ctrl-C kills this process instead of the command
	// inside, and nothing arrives until Enter is pressed.
	//
	// Restored on every exit path including a panic, because a terminal left in
	// raw mode is a shell that echoes nothing and needs `reset` to recover —
	// and the person it happens to is in the middle of an incident.
	if fd := int(opts.In.Fd()); term.IsTerminal(fd) {
		restore, err := term.MakeRaw(fd)
		if err != nil {
			return fmt.Errorf("putting this terminal into raw mode: %w", err)
		}
		defer func() { _ = term.Restore(fd, restore) }()

		c.watchResize(ctx, conn, fd)
	}

	return c.pump(ctx, cancel, conn, opts)
}

// execURL builds the websocket URL.
func (c *Client) execURL(opts ExecOptions) (string, error) {
	u, err := url.Parse(strings.TrimSuffix(c.BaseURL, "/") + "/apps/" + opts.AppID + "/exec")
	if err != nil {
		return "", fmt.Errorf("building the address for this app: %w", err)
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	default:
		u.Scheme = "ws"
	}

	q := u.Query()
	if opts.Workload != "" {
		q.Set("workload", opts.Workload)
	}
	// Repeated rather than joined: a command is a list of arguments, and
	// joining it on spaces would silently reinterpret `sh -c "a b"` as three
	// arguments the first time someone passed a quoted string.
	for _, arg := range opts.Command {
		q.Add("cmd", arg)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// watchResize tells the server the terminal's size, now and whenever it
// changes.
//
// Sent once up front because the container's shell has no way to ask: without
// it, everything is laid out for an 80x24 terminal regardless of the window,
// and a full-screen editor draws over itself.
func (c *Client) watchResize(ctx context.Context, conn *websocket.Conn, fd int) {
	send := func() {
		cols, rows, err := term.GetSize(fd)
		if err != nil || rows <= 0 || cols <= 0 {
			return
		}
		body, err := json.Marshal(control{Rows: uint16(rows), Cols: uint16(cols)})
		if err != nil {
			return
		}
		_ = conn.Write(ctx, websocket.MessageText, body)
	}
	send()

	sigwinch := make(chan os.Signal, 1)
	signal.Notify(sigwinch, syscall.SIGWINCH)
	go func() {
		defer signal.Stop(sigwinch)
		for {
			select {
			case <-ctx.Done():
				return
			case <-sigwinch:
				send()
			}
		}
	}()
}

// pump copies bytes both ways until one side ends.
func (c *Client) pump(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, opts ExecOptions) error {
	// Local input to the socket, in its own goroutine: a read from a terminal
	// blocks until a key is pressed, and the session has to keep rendering
	// output while nobody is typing.
	//
	// Not waited on. os.Stdin's Read cannot be canceled, so this goroutine
	// outlives the session by design — the process is about to exit, and the
	// alternative is hanging until the user presses a key.
	go func() {
		buf := make([]byte, 32<<10)
		for {
			n, err := opts.In.Read(buf)
			if n > 0 {
				if writeErr := conn.Write(ctx, websocket.MessageBinary, buf[:n]); writeErr != nil {
					return
				}
			}
			if err != nil {
				// Ctrl-D closes the input side and leaves the output side open,
				// so the shell's goodbye still arrives.
				_ = conn.Write(ctx, websocket.MessageBinary, nil)
				return
			}
		}
	}()

	for {
		kind, data, err := conn.Read(ctx)
		if err != nil {
			cancel()
			return execCloseError(err)
		}
		if kind != websocket.MessageBinary {
			continue
		}
		if _, err := opts.Out.Write(data); err != nil {
			// Reported, not swallowed. A terminal that stops rendering and
			// exits 0 tells someone mid-incident that their command finished.
			cancel()
			return fmt.Errorf("writing the session's output: %w", err)
		}
	}
}

// exitError carries a non-zero status out of the container.
//
// A separate type so the command can set the process's own exit status without
// printing a Go error: `pando exec app -- false` should behave like `false`,
// not like a failed command.
type exitError struct{ code int }

func (e *exitError) Error() string { return "exited with status " + strconv.Itoa(e.code) }
func (e *exitError) ExitCode() int { return e.code }

// execCloseError turns the socket's close into the session's result.
//
// The server closes with "exited with status N" as the reason, which is the
// only channel a websocket offers for it. Parsing a string is unpleasant and is
// what the protocol provides.
func execCloseError(err error) error {
	var closeErr websocket.CloseError
	if !errors.As(err, &closeErr) {
		if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
			return nil
		}
		return fmt.Errorf("the terminal session ended unexpectedly: %w", err)
	}

	switch closeErr.Code {
	case websocket.StatusNormalClosure, websocket.StatusGoingAway:
		if code, ok := strings.CutPrefix(closeErr.Reason, "exited with status "); ok {
			if n, convErr := strconv.Atoi(code); convErr == nil && n != 0 {
				return &exitError{code: n}
			}
		}
		return nil
	default:
		// The server's own message, which names the reason — the runtime
		// cannot exec, the workload is not declared, exec is disabled by
		// policy. Replacing it would delete the only useful thing here.
		if closeErr.Reason != "" {
			return errors.New(closeErr.Reason)
		}
		return fmt.Errorf("the terminal session was refused (%s)", closeErr.Code)
	}
}

// execDialError explains a refused upgrade.
//
// The handler rejects before upgrading — wrong verb, exec disabled by policy,
// app never deployed — so the failure arrives as an HTTP status rather than a
// close frame, and the body carries the error envelope.
func execDialError(err error, resp *http.Response) error {
	if resp == nil {
		return fmt.Errorf("opening a terminal: %w", err)
	}
	var envelope struct {
		Error struct {
			Message string `json:"message"`
			Remedy  string `json:"remedy"`
		} `json:"error"`
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if json.Unmarshal(body, &envelope) == nil && envelope.Error.Message != "" {
		if envelope.Error.Remedy != "" {
			return fmt.Errorf("%s\n%s", envelope.Error.Message, envelope.Error.Remedy)
		}
		return errors.New(envelope.Error.Message)
	}
	return fmt.Errorf("opening a terminal: %s", resp.Status)
}
