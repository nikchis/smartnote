package cloudhttp_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/nikchis/smartnote/internal/adapter/cloudhttp"
	"github.com/nikchis/smartnote/internal/domain"
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
		{name: "deadline", err: &net.OpError{Op: "read", Net: "tcp", Err: os.ErrDeadlineExceeded}, want: cloudhttp.NetTimeout},
		{name: "reset", err: opErr(syscall.ECONNRESET), want: cloudhttp.NetReset},
		{name: "refused", err: &net.OpError{Op: "dial", Net: "tcp", Err: &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED}}, want: cloudhttp.NetRefused},
		{name: "eof", err: io.EOF, want: cloudhttp.NetEOF},
		{name: "unexpected eof", err: fmt.Errorf("read body: %w", io.ErrUnexpectedEOF), want: cloudhttp.NetEOF},
		{name: "dns", err: &net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "no such host", Name: "x", IsNotFound: true}}, want: cloudhttp.NetDNS},
		{name: "dns timeout", err: &net.DNSError{Err: "timeout", Name: "x", IsTimeout: true}, want: cloudhttp.NetDNS},
		{name: "unknown authority", err: &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}, want: cloudhttp.NetTLS},
		{name: "hostname", err: x509.HostnameError{Host: "x"}, want: cloudhttp.NetTLS},
		{name: "record header", err: tls.RecordHeaderError{Msg: "first record does not look like a TLS handshake"}, want: cloudhttp.NetTLS},
		{name: "alert", err: fmt.Errorf("remote error: %w", tls.AlertError(40)), want: cloudhttp.NetTLS},
		{name: "other", err: errors.New("something else"), want: cloudhttp.NetOther},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := cloudhttp.NetClass(tc.err); got != tc.want {
				t.Errorf("NetClass(%v) = %q, want %q", tc.err, got, tc.want)
			}

			ce := cloudhttp.NewNetworkError(domain.KindYandexDisk, tc.err)
			if ce.Net != tc.want || ce.Code != cloudhttp.CodeNetwork {
				t.Errorf("NewNetworkError = %+v, want network %s", ce, tc.want)
			}
		})
	}
}

func TestCloudErrorCarriesProvider(t *testing.T) {
	ce := cloudhttp.NewAnswerError(domain.KindYandexDisk, 507, "x", cloudhttp.ReasonNoSpace)
	if ce.Provider != domain.KindYandexDisk || ce.Status != 507 || ce.Code != "x" || ce.Reason != cloudhttp.ReasonNoSpace {
		t.Errorf("NewAnswerError = %+v", ce)
	}

	if n := cloudhttp.NewNetworkError(domain.StorageKind("google_drive"), io.EOF); n.Provider != domain.StorageKind("google_drive") {
		t.Errorf("network provider = %q", n.Provider)
	}
}

func TestCloudErrorText(t *testing.T) {
	answer := cloudhttp.NewAnswerError(domain.KindYandexDisk, 503, "DiskInternalError", cloudhttp.ReasonNone)

	tests := []struct {
		name string
		err  cloudhttp.CloudError
		want string
	}{
		{name: "answer", err: answer, want: "yandex_disk error: 503 DiskInternalError"},
		{name: "answer at a step", err: cloudhttp.WithStep(answer, "upload"), want: "yandex_disk error: 503 DiskInternalError at upload"},
		{name: "network", err: cloudhttp.WithStep(cloudhttp.NewNetworkError(domain.KindYandexDisk, syscall.ECONNRESET), "download"), want: "yandex_disk error: 0 network at download (reset)"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Error(); got != tc.want {
				t.Errorf("Error() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNetworkErrorTimeoutIsNotContextDeadline(t *testing.T) {
	ce := cloudhttp.NewNetworkError(domain.KindYandexDisk, fmt.Errorf("x: %w", context.DeadlineExceeded))
	if errors.Is(ce, context.DeadlineExceeded) {
		t.Error("a transport timeout matches context.DeadlineExceeded")
	}

	if ce.Net != cloudhttp.NetTimeout {
		t.Errorf("Net = %q, want timeout", ce.Net)
	}
}

func TestRequestError(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := cloudhttp.RequestError(ctx, domain.KindYandexDisk, io.EOF, "stat")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("done ctx: err = %v, want context.Canceled", err)
	}

	err = cloudhttp.RequestError(t.Context(), domain.KindYandexDisk, io.EOF, "stat")

	ce, ok := errors.AsType[cloudhttp.CloudError](err)
	if !ok || ce.Step != "stat" || ce.Net != cloudhttp.NetEOF {
		t.Errorf("live ctx: err = %v, want a network CloudError at stat", err)
	}
}

func TestIsStatus(t *testing.T) {
	err := fmt.Errorf("w: %w", cloudhttp.NewAnswerError(domain.KindYandexDisk, 404, "x", cloudhttp.ReasonNone))
	if !cloudhttp.IsStatus(err, 404) || cloudhttp.IsStatus(err, 409) || cloudhttp.IsStatus(io.EOF, 404) {
		t.Error("IsStatus is wrong")
	}
}

func TestDefaultTransportIdleTimeout(t *testing.T) {
	tr, ok := cloudhttp.DefaultHTTP().Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, want *http.Transport", cloudhttp.DefaultHTTP().Transport)
	}

	if tr.IdleConnTimeout != 30*time.Second {
		t.Errorf("IdleConnTimeout = %v, want 30s", tr.IdleConnTimeout)
	}

	if tr.TLSClientConfig == nil || tr.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Error("TLS 1.2 is not the minimum")
	}
}

func TestWithRedirectsCopiesBase(t *testing.T) {
	base := &http.Client{}
	c := cloudhttp.WithRedirects(base, func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse })

	if base.CheckRedirect != nil || c.CheckRedirect == nil {
		t.Error("WithRedirects must set the policy on a copy only")
	}

	if cloudhttp.WithRedirects(nil, nil).Transport == nil {
		t.Error("nil base must take the default client")
	}
}
