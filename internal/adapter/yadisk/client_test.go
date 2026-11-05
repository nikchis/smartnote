package yadisk

import (
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
