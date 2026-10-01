package egress

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// fakeResolver answers from a table and counts what it was asked.
type fakeResolver struct {
	mu      sync.Mutex
	answers map[string][]netip.Addr
	asked   []string
}

func (f *fakeResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, host)
	a, ok := f.answers[host]
	if !ok {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	return a, nil
}

func (f *fakeResolver) lookups(host string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, h := range f.asked {
		if h == host {
			n++
		}
	}
	return n
}

// harness is a gateway whose resolver and dialer are fakes. Destinations
// resolve to documentation addresses, and the dialer connects whatever
// address it is given to a real listener on loopback — recording the address,
// which is what the rules were checked against.
type harness struct {
	t        *testing.T
	gw       *Gateway
	url      string
	resolver *fakeResolver
	logs     *observer.ObservedLogs

	mu     sync.Mutex
	dialed []string
	routes map[string]string // address the gateway dials -> real listener
}

func newHarness(t *testing.T, rules Rules) *harness {
	t.Helper()
	core, logs := observer.New(zapcore.DebugLevel)
	h := &harness{
		t:        t,
		resolver: &fakeResolver{answers: map[string][]netip.Addr{}},
		logs:     logs,
		routes:   map[string]string{},
	}
	h.gw = &Gateway{Rules: compile(t, rules), Logger: zap.New(core), Resolver: h.resolver, DialTimeout: 2 * time.Second}
	h.gw.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		h.mu.Lock()
		h.dialed = append(h.dialed, address)
		real, ok := h.routes[address]
		h.mu.Unlock()
		if !ok {
			return nil, &net.OpError{Op: "dial", Net: network, Err: errors.New("connection refused")}
		}
		var d net.Dialer
		return d.DialContext(ctx, network, real)
	}
	srv := httptest.NewServer(h.gw)
	t.Cleanup(srv.Close)
	h.url = srv.URL
	return h
}

// route makes name resolve to addr, and connections to addr:port reach real.
func (h *harness) route(name, addr string, port int, real string) {
	a := netip.MustParseAddr(addr)
	h.mu.Lock()
	defer h.mu.Unlock()
	if name != "" {
		h.resolver.answers[name] = append(h.resolver.answers[name], a)
	}
	h.routes[netip.AddrPortFrom(a, uint16(port)).String()] = real //nolint:gosec // G115: a test port.
}

func (h *harness) dials() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.dialed...)
}

// connect sends CONNECT target and returns the connection, the status line
// and, for a refusal, the body.
func (h *harness) connect(target string) (net.Conn, *bufio.Reader, *http.Response) {
	h.t.Helper()
	conn, err := net.Dial("tcp", strings.TrimPrefix(h.url, "http://"))
	require.NoError(h.t, err)
	h.t.Cleanup(func() { _ = conn.Close() })
	_, err = io.WriteString(conn, "CONNECT "+target+" HTTP/1.1\r\nHost: "+target+"\r\n\r\n")
	require.NoError(h.t, err)
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	require.NoError(h.t, err)
	return conn, br, resp
}

func body(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	_ = resp.Body.Close()
	return string(b)
}

// echoServer accepts connections on loopback and writes back what it reads.
func echoServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = c.Close() }()
				_, _ = io.Copy(c, c)
			}()
		}
	}()
	return ln.Addr().String()
}

// proxied returns an HTTP client that sends everything through the gateway.
func (h *harness) client() *http.Client {
	u, _ := url.Parse(h.url)
	return &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(u)},
		Timeout:   5 * time.Second,
	}
}

// TestR187_TheGatewayTunnelsToAnAllowedDestination asserts R-187: HTTPS
// leaves a restricted app as a CONNECT tunnel through the gateway, to a
// destination its rules allow.
func TestR187_TheGatewayTunnelsToAnAllowedDestination(t *testing.T) {
	t.Parallel()
	h := newHarness(t, Rules{Layers: []Layer{{Mode: Allowlist, From: "install", List: []string{"api.example.com:443"}}}})
	h.route("api.example.com", "203.0.113.10", 443, echoServer(t))

	conn, br, resp := h.connect("api.example.com:443")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()

	_, err := io.WriteString(conn, "hello through the tunnel\n")
	require.NoError(t, err)
	line, err := br.ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "hello through the tunnel\n", line)
	require.Equal(t, []string{"203.0.113.10:443"}, h.dials())
}

// A client may send its first bytes with the CONNECT, before the reply, and
// close its sending side once it has. Those bytes go through the tunnel; they
// are not read as a second request, and the hang-up does not cancel the dial.
func TestR187_ATunnelCarriesBytesSentBeforeItsReply(t *testing.T) {
	t.Parallel()
	h := newHarness(t, Rules{Layers: []Layer{{Mode: Allowlist, List: []string{"api.example.com"}}}})
	h.route("api.example.com", "203.0.113.11", 443, echoServer(t))

	conn, err := net.Dial("tcp", strings.TrimPrefix(h.url, "http://"))
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	_, err = io.WriteString(conn, "CONNECT api.example.com:443 HTTP/1.1\r\nHost: api.example.com:443\r\n\r\nearly bytes\n")
	require.NoError(t, err)
	require.NoError(t, conn.(*net.TCPConn).CloseWrite())

	got, err := io.ReadAll(conn)
	require.NoError(t, err)
	require.Equal(t, "HTTP/1.1 200 Connection Established\r\n\r\nearly bytes\n", string(got))
}

// TestR185_TheGatewayRefusesWhatTheRulesRefuse asserts R-185 and R-105: a
// refused CONNECT is a 403 that says why, and a structured log line naming
// the host, the port and the reason.
func TestR185_TheGatewayRefusesWhatTheRulesRefuse(t *testing.T) {
	t.Parallel()
	h := newHarness(t, Rules{Layers: []Layer{{Mode: Denylist, From: "install", List: []string{"tracker.example"}}}})
	h.route("tracker.example", "203.0.113.20", 443, echoServer(t))

	_, _, resp := h.connect("tracker.example:443")
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
	require.Equal(t,
		"Pando's egress gateway refused a connection to tracker.example:443: tracker.example is on the installation's denylist (tracker.example). "+
			"An app's egress rules come from host policy and the app's own settings, and take effect at its next deploy.\n",
		body(t, resp))
	require.Zero(t, h.resolver.lookups("tracker.example"), "a denied name is never resolved")
	require.Empty(t, h.dials())

	refused := h.logs.FilterMessage("egress refused").All()
	require.Len(t, refused, 1)
	fields := refused[0].ContextMap()
	require.Equal(t, "tracker.example", fields["host"])
	require.EqualValues(t, 443, fields["port"])
	require.Contains(t, fields["reason"], "denylist")
}

// TestR187_TheGatewayForwardsPlainHTTP asserts R-187: plain HTTP leaves as an
// absolute-form request to the gateway, which HTTP_PROXY produces.
func TestR187_TheGatewayForwardsPlainHTTP(t *testing.T) {
	t.Parallel()
	var gotHost, gotProxyAuth, gotXFF string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost, gotProxyAuth, gotXFF = r.Host, r.Header.Get("Proxy-Authorization"), r.Header.Get("X-Forwarded-For")
		_, _ = io.WriteString(w, "hello from "+r.URL.Path)
	}))
	t.Cleanup(backend.Close)

	h := newHarness(t, Rules{Layers: []Layer{{Mode: Allowlist, List: []string{"web.example"}}}})
	h.route("web.example", "203.0.113.30", 80, strings.TrimPrefix(backend.URL, "http://"))

	req, _ := http.NewRequest(http.MethodGet, "http://web.example/path", nil)
	req.Header.Set("Proxy-Authorization", "Basic c2VjcmV0")
	resp, err := h.client().Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "hello from /path", body(t, resp))
	require.Equal(t, "web.example", gotHost, "the Host the app asked for is kept")
	require.Empty(t, gotProxyAuth, "credentials for the proxy stop at the proxy")
	require.Empty(t, gotXFF, "nothing about the app's network is added")
	require.Equal(t, []string{"203.0.113.30:80"}, h.dials())

	// And refused like a tunnel is.
	resp, err = h.client().Get("http://elsewhere.example/")
	require.NoError(t, err)
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
	require.Contains(t, body(t, resp), "elsewhere.example on port 80 is not on the egress allowlist")
}

// TestR185_TheGatewayDialsOnlyAddressesTheRulesAllowed asserts R-185: block
// private ranges is checked against where a name resolved, and the
// connection goes to an address that was checked — the name is never
// resolved again, so it cannot answer differently the second time.
func TestR185_TheGatewayDialsOnlyAddressesTheRulesAllowed(t *testing.T) {
	t.Parallel()
	h := newHarness(t, Rules{BlockPrivate: true})
	echo := echoServer(t)
	h.route("mixed.example", "169.254.169.254", 443, echo)
	h.route("mixed.example", "203.0.113.40", 443, echo)

	conn, br, resp := h.connect("mixed.example:443")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()
	_, _ = io.WriteString(conn, "x\n")
	_, err := br.ReadString('\n')
	require.NoError(t, err)

	require.Equal(t, []string{"203.0.113.40:443"}, h.dials(), "the private address is never dialed")
	require.Equal(t, 1, h.resolver.lookups("mixed.example"), "resolved once, and dialed by address")

	// A name that resolves only to private addresses is refused, and says so.
	h.route("rebind.example", "10.0.0.5", 443, echo)
	_, _, resp = h.connect("rebind.example:443")
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
	require.Contains(t, body(t, resp), "rebind.example resolves to 10.0.0.5, a private address, and private addresses are blocked")

	// And so is a private address asked for directly.
	_, _, resp = h.connect("[::ffff:192.168.1.1]:443")
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
	require.Contains(t, body(t, resp), "192.168.1.1 is a private address")
}

// Without private-range blocking, a private destination the rules allow is
// reachable: blocking is a switch, not a side effect of restriction.
func TestR181_APrivateDestinationIsReachableWhenNotBlocked(t *testing.T) {
	t.Parallel()
	h := newHarness(t, Rules{Layers: []Layer{{Mode: Denylist, List: []string{"tracker.example"}}}})
	h.route("db.internal", "10.0.0.5", 5432, echoServer(t))

	_, _, resp := h.connect("db.internal:5432")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()
	require.Equal(t, []string{"10.0.0.5:5432"}, h.dials())
}

// The gateway's own loopback is never a destination, whatever the rules say.
func TestTheGatewayNeverConnectsToItself(t *testing.T) {
	t.Parallel()
	h := newHarness(t, Rules{Layers: []Layer{{Mode: Allowlist, List: []string{"*"}}}})
	h.route("localhost", "127.0.0.1", 3128, echoServer(t))

	for _, target := range []string{"127.0.0.1:3128", "[::1]:3128", "localhost:3128", "0.0.0.0:3128"} {
		_, _, resp := h.connect(target)
		require.Equal(t, http.StatusForbidden, resp.StatusCode, target)
		require.Contains(t, body(t, resp), "the gateway's own address", target)
	}
	require.Empty(t, h.dials())
}

func TestTheGatewayAnswersWhatItCannotDo(t *testing.T) {
	t.Parallel()
	h := newHarness(t, Rules{Layers: []Layer{{Mode: Allowlist, List: []string{"*"}}}})

	t.Run("health", func(t *testing.T) {
		resp, err := http.Get(h.url + "/healthz")
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		require.Equal(t, "ok\n", body(t, resp))
	})

	t.Run("a request that is not proxied", func(t *testing.T) {
		resp, err := http.Get(h.url + "/something")
		require.NoError(t, err)
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
		require.Contains(t, body(t, resp), "Set HTTP_PROXY and HTTPS_PROXY")
	})

	t.Run("a CONNECT without a port", func(t *testing.T) {
		_, _, resp := h.connect("api.example.com")
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
		require.Contains(t, body(t, resp), "host:port")
	})

	t.Run("a name that does not resolve", func(t *testing.T) {
		_, _, resp := h.connect("nowhere.example:443")
		require.Equal(t, http.StatusBadGateway, resp.StatusCode)
		require.Contains(t, body(t, resp), "could not find an address for nowhere.example")
	})

	t.Run("a destination that does not answer", func(t *testing.T) {
		h.resolver.mu.Lock()
		h.resolver.answers["down.example"] = []netip.Addr{netip.MustParseAddr("203.0.113.99")}
		h.resolver.mu.Unlock()
		_, _, resp := h.connect("down.example:443")
		require.Equal(t, http.StatusBadGateway, resp.StatusCode)
		require.Equal(t, "Pando's egress gateway could not connect to down.example:443: connection refused.\n", body(t, resp))
	})

	t.Run("an https URL sent as a plain request", func(t *testing.T) {
		conn, err := net.Dial("tcp", strings.TrimPrefix(h.url, "http://"))
		require.NoError(t, err)
		defer func() { _ = conn.Close() }()
		_, _ = io.WriteString(conn, "GET https://api.example.com/ HTTP/1.1\r\nHost: api.example.com\r\n\r\n")
		resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
		require.NoError(t, err)
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
		require.Contains(t, body(t, resp), "CONNECT")
	})
}

// A tunnel can stay open for hours; stopping the gateway closes it rather
// than waiting for it.
func TestStoppingTheGatewayClosesItsTunnels(t *testing.T) {
	t.Parallel()
	echo := echoServer(t)
	gw := &Gateway{
		Rules:    compile(t, Rules{BlockPrivate: true}),
		Resolver: &fakeResolver{answers: map[string][]netip.Addr{"api.example": {netip.MustParseAddr("203.0.113.50")}}},
	}
	gw.dial = func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, echo)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- gw.Serve(ctx, ln) }()

	conn, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	_, _ = io.WriteString(conn, "CONNECT api.example:443 HTTP/1.1\r\nHost: api.example:443\r\n\r\n")
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()

	cancel()
	select {
	case err := <-served:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("the gateway did not stop")
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, err = br.ReadByte()
	require.ErrorIs(t, err, io.EOF, "the tunnel was closed")
}
