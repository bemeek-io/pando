package source

import (
	"net"
	"net/http"
	"time"

	"github.com/go-git/go-git/v5/plumbing/transport/client"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
)

// gitHTTPClient is the client every git fetch over HTTP uses.
//
// go-git's default has no timeouts of any kind. After a while in use, the first
// request of every clone — `GET …/info/refs` — hung until the ten-minute
// detection deadline, while the same URL answered in a quarter of a second from
// inside the same container, and restarting Pando cleared it (issue #55). That
// is a pooled connection that died without closing: every clone to the host is
// multiplexed onto one HTTP/2 connection, and with no health check nothing ever
// notices it is gone.
//
// So: HTTP/2 pings a connection that has gone quiet and drops it when the ping
// is not answered, a server that accepts a request and never starts answering
// is given up on, and idle connections are not kept long enough to go stale.
// None of these bound a clone that is making progress — a large repository
// still streams for as long as it takes, under the caller's context.
var gitHTTPClient = &http.Client{
	Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   30 * time.Second,
		ResponseHeaderTimeout: 2 * time.Minute,
		IdleConnTimeout:       60 * time.Second,
		MaxIdleConnsPerHost:   4,
		HTTP2: &http.HTTP2Config{
			SendPingTimeout: 30 * time.Second,
			PingTimeout:     15 * time.Second,
		},
	},
}

func init() {
	c := githttp.NewClient(gitHTTPClient)
	client.InstallProtocol("https", c)
	client.InstallProtocol("http", c)
}
