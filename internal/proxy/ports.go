package proxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"
)

// PortListeners serves each port-mode app at the root of its own host port.
//
// R-166 prefers subdomain and falls back to path, and the loopback adapter that
// ships as the laptop default supports neither — it is port mode only, which is
// why a port is allocated at all (design 03 §4.2, O-15). The allocation was
// being made and written into the spec, and the console was showing the address
// it produced, but nothing ever listened on it: every laptop install told its
// users about an address that refused the connection, and the apps were in fact
// reachable only under the path prefix.
//
// That is not a cosmetic gap. Under a path prefix an app that writes absolute
// addresses into its own HTML — which is every frontend built with a default
// configuration — comes up blank, and Pando will not rewrite the response to
// paper over it (R-167, R-028). Port mode is the answer to exactly that, and it
// only exists if something accepts connections.
//
// Each listener is an ordinary http.Server whose handler is the same Proxy as
// every other request path. There is no second enforcement point and no fast
// path: the only thing the listener contributes is the port the request arrived
// on, which resolve() turns into an app (R-023).
type PortListeners struct {
	// Ports reports which host ports are currently allocated to apps.
	Ports interface {
		InUse(ctx context.Context) ([]int, error)
	}

	// Handler is the proxy. Taken as an interface so a test can assert on what
	// reached it without standing up the whole request path.
	Handler http.Handler

	// Interval is how often the set of listeners is reconciled against the
	// allocations. Allocation happens during detection, which is neither
	// frequent nor latency-sensitive.
	Interval time.Duration

	Logger *zap.Logger

	mu      sync.Mutex
	serving map[int]*http.Server
}

// Run reconciles listeners until ctx is canceled, then closes all of them.
func (l *PortListeners) Run(ctx context.Context) {
	interval := l.Interval
	if interval <= 0 {
		interval = 15 * time.Second
	}

	l.sync(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			l.closeAll()
			return
		case <-ticker.C:
			l.sync(ctx)
		}
	}
}

// sync opens a listener for every allocated port and closes the rest.
func (l *PortListeners) sync(ctx context.Context) {
	ports, err := l.Ports.InUse(ctx)
	if err != nil {
		// Tried again on the next tick. Tearing down every app's address
		// because one query failed would turn a blip into an outage.
		l.log().Warn("could not read the ports apps are using", zap.Error(err))
		return
	}

	wanted := make(map[int]bool, len(ports))
	for _, port := range ports {
		wanted[port] = true
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.serving == nil {
		l.serving = map[int]*http.Server{}
	}

	for port, srv := range l.serving {
		if !wanted[port] {
			// The app that held this port is gone. Its address stops answering
			// rather than answering for whoever is handed the port next.
			_ = srv.Close()
			delete(l.serving, port)
		}
	}

	for port := range wanted {
		if l.serving[port] != nil {
			continue
		}
		srv, err := l.listen(port)
		if err != nil {
			// Almost always something else on the host already holding the
			// port. Logged once per tick rather than fatal: one app's address
			// not working is not a reason for Pando not to run, and the rest of
			// the install is unaffected.
			l.log().Warn("could not open an app's port", zap.Int("port", port), zap.Error(err))
			continue
		}
		l.serving[port] = srv
	}
}

// listen opens one port and starts serving it.
func (l *PortListeners) listen(port int) (*http.Server, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, err
	}

	srv := &http.Server{
		Handler: l.Handler,
		// How the request finds its app. Stamped on the connection rather than
		// read from the Host header, because the port a connection arrived on
		// is a fact about the socket and a Host header is a claim by the client.
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			return withLocalPort(ctx, portOf(c.LocalAddr()))
		},
		ReadHeaderTimeout: 30 * time.Second,
	}

	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			l.log().Warn("an app's port stopped serving", zap.Int("port", port), zap.Error(err))
		}
	}()
	return srv, nil
}

func (l *PortListeners) closeAll() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for port, srv := range l.serving {
		_ = srv.Close()
		delete(l.serving, port)
	}
}

func (l *PortListeners) log() *zap.Logger {
	if l.Logger == nil {
		return zap.NewNop()
	}
	return l.Logger
}

// ctxKeyLocalPort carries the port a request arrived on.
type ctxKeyLocalPort struct{}

func withLocalPort(ctx context.Context, port int) context.Context {
	if port == 0 {
		return ctx
	}
	return context.WithValue(ctx, ctxKeyLocalPort{}, port)
}

func localPort(ctx context.Context) (int, bool) {
	port, ok := ctx.Value(ctxKeyLocalPort{}).(int)
	return port, ok && port > 0
}

func portOf(addr net.Addr) int {
	if tcp, ok := addr.(*net.TCPAddr); ok {
		return tcp.Port
	}
	return 0
}
