package cloudhttp

import (
	"crypto/tls"
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	// idleConnTimeout closes a kept connection that no request used for this
	// long: a long-idle connection may already be dropped by the network.
	idleConnTimeout = 30 * time.Second

	// dialTimeout bounds the name lookup and TCP connection of every request
	// (to the proxy, when there is one). A node that does not accept a
	// connection fails the request within it.
	dialTimeout = 10 * time.Second

	// dialKeepAlive is the TCP keep-alive period, as in the standard transport.
	dialKeepAlive = 30 * time.Second
)

var defaultHTTP = sync.OnceValue(func() *http.Client {
	t := &http.Transport{Proxy: http.ProxyFromEnvironment}
	if dt, ok := http.DefaultTransport.(*http.Transport); ok {
		t = dt.Clone()
	}

	t.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	t.IdleConnTimeout = idleConnTimeout
	t.DialContext = (&net.Dialer{Timeout: dialTimeout, KeepAlive: dialKeepAlive}).DialContext

	return &http.Client{Transport: t}
})

// DefaultHTTP is the base client of the cloud requests: system roots, TLS 1.2
// or later and the proxy from the environment. One transport serves all
// stores, so connections are reused within an operation; the store closes
// the idle ones at its end, and the idle timeout bounds their life.
func DefaultHTTP() *http.Client { return defaultHTTP() }

// WithRedirects returns a copy of base, or of the default client, with the
// redirect policy check.
func WithRedirects(base *http.Client, check func(*http.Request, []*http.Request) error) *http.Client {
	if base == nil {
		base = DefaultHTTP()
	}

	c := *base
	c.CheckRedirect = check

	return &c
}
