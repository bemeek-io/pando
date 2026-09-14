package proxy

import (
	"bufio"
	"context"
	"encoding/binary"
	"net"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/bemeek-io/pando/internal/core/assertion"
	"github.com/bemeek-io/pando/internal/core/authz"
	"github.com/bemeek-io/pando/internal/log"
)

// reauthorizing wraps a ResponseWriter so that a hijacked connection — a
// websocket, almost always — is re-authorized for as long as it stays open.
//
// O-13. The per-request CheckData that enforces access never fires again once a
// connection is upgraded, so without this a websocket would be the one way to
// hold access indefinitely after revocation. That is precisely the property an
// attacker looks for.
type reauthorizing struct {
	http.ResponseWriter

	proxy     *Proxy
	principal authz.Principal
	appID     string
	ctx       context.Context
}

// Hijack takes over the connection and starts the re-authorization loop.
func (w *reauthorizing) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}

	conn, rw, err := hijacker.Hijack()
	if err != nil {
		return nil, nil, err
	}

	go w.watch(conn)
	return conn, rw, nil
}

// watch re-runs CheckData on the assertion lifetime and closes on failure.
//
// The interval is assertion.Lifetime itself, not a copy of its value: the
// revocation window, the assertion's life and this timer are one number, and
// referencing the constant is what keeps them from drifting apart.
func (w *reauthorizing) watch(conn net.Conn) {
	ticker := time.NewTicker(assertion.Lifetime)
	defer ticker.Stop()

	// Detached from the request context: the request is over the moment the
	// connection is hijacked, and a canceled context would end the loop
	// immediately, which is the opposite of what this is for.
	ctx := context.WithoutCancel(w.ctx)

	for range ticker.C {
		if err := w.proxy.Authz.CheckData(ctx, w.principal, w.appID); err == nil {
			continue
		}

		log.From(ctx).Info("closing a long-lived connection after revocation",
			zap.String("app_id", w.appID),
			zap.String("principal_id", w.principal.ID))

		// A policy-violation close frame rather than an abrupt reset, so a
		// client can tell revocation from a network fault and does not simply
		// reconnect in a loop.
		_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
		_, _ = conn.Write(closeFrame(closePolicyViolation, "access revoked"))
		_ = conn.Close()
		return
	}
}

// WebSocket close codes.
const closePolicyViolation = 1008

// closeFrame builds an unmasked server-to-client close frame (RFC 6455 §5.5.1).
//
// Written by hand because the proxy does not otherwise speak the websocket
// protocol — it hands bytes along — and pulling in a websocket library to send
// two frames' worth of bytes would be a dependency for nothing.
func closeFrame(code uint16, reason string) []byte {
	payload := make([]byte, 2+len(reason))
	binary.BigEndian.PutUint16(payload, code)
	copy(payload[2:], reason)

	// Close payloads are capped at 125 bytes, which is also the boundary below
	// which the length fits in the header's 7 bits — so no extended length.
	if len(payload) > 125 {
		payload = payload[:125]
	}

	frame := make([]byte, 0, 2+len(payload))
	frame = append(frame, 0x88) // FIN + opcode 8 (close)
	// G115: payload was truncated to 125 bytes immediately above, which is the
	// reason the truncation is there.
	frame = append(frame, byte(len(payload))) //nolint:gosec
	return append(frame, payload...)
}

// Unwrap lets http.ResponseController reach Flush on the underlying writer.
// Without it, wrapping would break SSE — the very thing R-170 requires.
func (w *reauthorizing) Unwrap() http.ResponseWriter { return w.ResponseWriter }
