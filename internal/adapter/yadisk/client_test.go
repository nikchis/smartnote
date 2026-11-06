package yadisk

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestNetClass(t *testing.T) {
	opErr := func(err error) error {
		return &net.OpError{Op: "read", Net: "tcp", Err: &os.SyscallError{Syscall: "read", Err: err}}
	}

	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "deadline", err: &net.OpError{Op: "read", Net: "tcp", Err: os.ErrDeadlineExceeded}, want: netTimeout},
		{name: "reset", err: opErr(syscall.ECONNRESET), want: netReset},
		{name: "refused", err: &net.OpError{Op: "dial", Net: "tcp", Err: &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED}}, want: netRefused},
		{name: "eof", err: io.EOF, want: netEOF},
		{name: "unexpected eof", err: fmt.Errorf("read body: %w", io.ErrUnexpectedEOF), want: netEOF},
		{name: "dns", err: &net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "no such host", Name: "x", IsNotFound: true}}, want: netDNS},
		{name: "dns timeout", err: &net.DNSError{Err: "timeout", Name: "x", IsTimeout: true}, want: netDNS},
		{name: "unknown authority", err: &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}, want: netTLS},
		{name: "hostname", err: x509.HostnameError{Host: "x"}, want: netTLS},
		{name: "record header", err: tls.RecordHeaderError{Msg: "first record does not look like a TLS handshake"}, want: netTLS},
		{name: "alert", err: fmt.Errorf("remote error: %w", tls.AlertError(40)), want: netTLS},
		{name: "other", err: errors.New("something else"), want: netOther},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := netClass(tc.err); got != tc.want {
				t.Errorf("netClass(%v) = %q, want %q", tc.err, got, tc.want)
			}

			if ce := networkError(tc.err); ce.Net != tc.want || ce.Code != codeNetwork {
				t.Errorf("networkError = %+v, want network %s", ce, tc.want)
			}
		})
	}
}

func TestCloudErrorText(t *testing.T) {
	tests := []struct {
		name string
		err  CloudError
		want string
	}{
		{name: "answer", err: CloudError{Status: 503, Code: "DiskInternalError"}, want: "yandex disk error: 503 DiskInternalError"},
		{name: "answer at a step", err: CloudError{Status: 503, Code: "DiskInternalError", Step: stepUpload}, want: "yandex disk error: 503 DiskInternalError at upload"},
		{name: "network", err: CloudError{Code: codeNetwork, Step: stepDownload, Net: netReset}, want: "yandex disk error: 0 network at download (reset)"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Error(); got != tc.want {
				t.Errorf("Error() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDefaultTransportIdleTimeout(t *testing.T) {
	tr, ok := defaultHTTP().Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, want *http.Transport", defaultHTTP().Transport)
	}

	if tr.IdleConnTimeout != 30*time.Second {
		t.Errorf("IdleConnTimeout = %v, want 30s", tr.IdleConnTimeout)
	}
}

func TestHrefClientSecureLink(t *testing.T) {
	c := newHrefClient(nil, nil)

	tests := []struct {
		name string
		href string
		ok   bool
	}{
		{name: "upload host with port", href: "https://uploader1.disk.yandex.net:443/upload-target/x?y=1", ok: true},
		{name: "download host", href: "https://downloader.disk.yandex.ru/disk/x", ok: true},
		{name: "storage host", href: "https://mds.storage.yandex.net/get/x", ok: true},
		{name: "upper case apex", href: "https://YANDEX.NET/x", ok: true},
		{name: "apex ru", href: "https://yandex.ru/x", ok: true},
		{name: "no dot before the suffix", href: "https://evilyandex.net/x"},
		{name: "suffix is not the end", href: "https://yandex.net.evil.com/x"},
		{name: "other zone", href: "https://yandex.com/x"},
		{name: "http", href: "http://x.yandex.ru/x"},
		{name: "userinfo hides the host", href: "https://x.yandex.net@evil.com/x"},
		{name: "trailing dot", href: "https://yandex.net./x"},
		{name: "no host", href: "https:///x"},
		{name: "IP address", href: "https://127.0.0.1/x"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.secureLink(tc.href)

			switch {
			case tc.ok && err != nil:
				t.Errorf("secureLink = %v, want accepted", err)
			case !tc.ok && !errors.Is(err, errInsecureLink):
				t.Errorf("secureLink = %v, want errInsecureLink", err)
			}

			if err != nil && strings.Contains(err.Error(), "evil") {
				t.Errorf("the error carries the link: %v", err)
			}
		})
	}
}

func TestMeterIgnoresReplacedBody(t *testing.T) {
	var reports []int64

	m := startMeter(func(done int64) { reports = append(reports, done) })
	old := m.wrap(bytes.NewReader(make([]byte, 100)))

	if _, err := io.CopyN(io.Discard, old, 60); err != nil {
		t.Fatal(err)
	}

	m.restart()

	fresh := m.wrap(bytes.NewReader(make([]byte, 100)))

	if _, err := io.CopyN(io.Discard, old, 40); err != nil { // the transport still reads the old body
		t.Fatal(err)
	}

	if _, err := io.Copy(io.Discard, fresh); err != nil {
		t.Fatal(err)
	}

	if last := reports[len(reports)-1]; last != 100 {
		t.Errorf("last report = %d, want 100 (reports %v)", last, reports)
	}

	for _, d := range reports {
		if d > 100 {
			t.Errorf("report %d exceeds the body length", d)
		}
	}
}

// stalledRequest sends a GET to a server that never answers, with hc.
func stalledRequest(ctx context.Context, t *testing.T, hc *http.Client) error {
	t.Helper()

	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))

	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}

	_, err = roundTrip(hc, req, stepStat, nil)

	return err
}

func TestRoundTripTransportTimeoutIsCloudError(t *testing.T) {
	err := stalledRequest(t.Context(), t, &http.Client{Timeout: 50 * time.Millisecond})

	ce, ok := errors.AsType[CloudError](err)
	if !ok {
		t.Fatalf("err = %v, want a CloudError", err)
	}

	if ce.Code != codeNetwork || ce.Net != netTimeout || ce.Step != stepStat {
		t.Errorf("CloudError = %+v, want network timeout at stat", ce)
	}

	if errors.Is(err, context.DeadlineExceeded) {
		t.Error("a transport timeout matches context.DeadlineExceeded")
	}
}

func TestRoundTripContextDeadlineIsContextError(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	err := stalledRequest(ctx, t, &http.Client{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want context.DeadlineExceeded", err)
	}

	if _, ok := errors.AsType[CloudError](err); ok {
		t.Errorf("err = %v, a context deadline became a CloudError", err)
	}
}

func TestRoundTripCanceledIsContextError(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(50*time.Millisecond, cancel)

	if err := stalledRequest(ctx, t, &http.Client{}); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestHrefGetTransportTimeoutAndContext(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))

	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })

	base := srv.Client()
	base.Timeout = 50 * time.Millisecond
	c := newHrefClient(base, func(string) bool { return true })

	_, err := c.get(t.Context(), srv.URL, 1024, nil)

	ce, ok := errors.AsType[CloudError](err)
	if !ok || ce.Code != codeNetwork || ce.Net != netTimeout || ce.Step != stepDownload {
		t.Errorf("live ctx: err = %v, want a network timeout CloudError at download", err)
	}

	if errors.Is(err, context.DeadlineExceeded) {
		t.Error("live ctx: a transport timeout matches context.DeadlineExceeded")
	}

	base.Timeout = 0

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	_, err = c.get(ctx, srv.URL, 1024, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expired ctx: err = %v, want context.DeadlineExceeded", err)
	}
}
