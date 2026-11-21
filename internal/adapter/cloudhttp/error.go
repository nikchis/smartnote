// Package cloudhttp is what the cloud adapters share: the error of a cloud
// request with its provider and reason, the classes of network errors, the
// HTTP client and the counter of transferred bytes. It does not know the
// answer formats of the providers: an adapter parses the body, picks the
// code and the Reason and builds the CloudError.
package cloudhttp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"syscall"

	"github.com/nikchis/smartnote/internal/domain"
)

// CodeNetwork is the Code of a request that got no answer.
const CodeNetwork = "network"

// Classes of a network error, CloudError.Net.
const (
	NetTimeout = "timeout"
	NetReset   = "reset"
	NetEOF     = "eof"
	NetDNS     = "dns"
	NetTLS     = "tls"
	NetRefused = "refused"
	NetOther   = "other"
)

// Reason is the known cause of an error answer, set by the adapter of the
// provider; the interface chooses its text by it.
type Reason string

// Reasons of a CloudError.
const (
	ReasonNone     Reason = ""
	ReasonNoSpace  Reason = "no_space"
	ReasonTooLarge Reason = "too_large"
	ReasonBusy     Reason = "busy"
)

var errTransportTimeout = errors.New("request timed out")

// CloudError is an error answer of a cloud (Status is the HTTP status) or a
// request without an answer (Status 0, Code "network"). Provider is the
// cloud it came from, Code a name from the closed list of the provider's
// adapter. Step names the request that failed, Net the class of a network
// error, Reason the known cause, if any. The text of the answer and the
// URLs of the request never get into it.
type CloudError struct {
	Provider domain.StorageKind
	Status   int
	Code     string
	Step     string
	Net      string
	Reason   Reason

	err error // why there is no answer, without the request URL
}

func (e CloudError) Error() string {
	text := fmt.Sprintf("%s error: %d %s", e.Provider, e.Status, e.Code)
	if e.Step != "" {
		text += " at " + e.Step
	}

	if e.Net != "" {
		text += " (" + e.Net + ")"
	}

	return text
}

// Unwrap returns the cause of a network error.
func (e CloudError) Unwrap() error { return e.err }

// NewAnswerError is the error of an answer of p with status, the code from
// the closed list of p and the reason r.
func NewAnswerError(p domain.StorageKind, status int, code string, r Reason) CloudError {
	return CloudError{Provider: p, Status: status, Code: code, Reason: r}
}

// NewNetworkError is a request without an answer. The *url.Error is dropped:
// its text has the request URL, which carries the path and, for a link, its
// signed parameters.
func NewNetworkError(p domain.StorageKind, err error) CloudError {
	if ue, ok := errors.AsType[*url.Error](err); ok {
		err = ue.Err
	}

	class := NetClass(err)
	if errors.Is(err, context.DeadlineExceeded) {
		// A timeout of the transport, not of the operation: the cause must
		// not match the deadline of a context, or the interface would show
		// it as the operation's own timeout.
		err = errTransportTimeout
	}

	return CloudError{Provider: p, Code: CodeNetwork, Net: class, err: err}
}

// WithStep returns ce marked with step.
func WithStep(ce CloudError, step string) CloudError {
	ce.Step = step
	return ce
}

// WithCause returns ce with err as the cause Unwrap gives; err must not
// quote a URL.
func WithCause(ce CloudError, err error) CloudError {
	ce.err = err
	return ce
}

// RequestError is the error of a request of step that got no answer. When
// the context of the request is done, the operation's own limit or a
// cancellation is the cause, and the context error is returned; otherwise it
// is a network CloudError of p.
func RequestError(ctx context.Context, p domain.StorageKind, err error, step string) error {
	if cerr := ctx.Err(); cerr != nil {
		return fmt.Errorf("%s: %w", step, cerr)
	}

	return WithStep(NewNetworkError(p, err), step)
}

// IsStatus reports whether err is a CloudError answer with status.
func IsStatus(err error, status int) bool {
	ce, ok := errors.AsType[CloudError](err)
	return ok && ce.Status == status
}

// NetClass is the class of the cause of a request without an answer, for
// the log: the cause itself may quote addresses.
func NetClass(err error) string {
	switch {
	case isDNS(err):
		return NetDNS
	case isTLS(err):
		return NetTLS
	case isTimeout(err):
		return NetTimeout
	case errors.Is(err, syscall.ECONNRESET):
		return NetReset
	case errors.Is(err, syscall.ECONNREFUSED):
		return NetRefused
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return NetEOF
	default:
		return NetOther
	}
}

func isDNS(err error) bool {
	_, ok := errors.AsType[*net.DNSError](err)
	return ok
}

func isTimeout(err error) bool {
	ne, ok := errors.AsType[net.Error](err)
	return ok && ne.Timeout()
}

// isTLS reports a failed TLS handshake or certificate check.
func isTLS(err error) bool {
	if _, ok := errors.AsType[*tls.CertificateVerificationError](err); ok {
		return true
	}

	if _, ok := errors.AsType[tls.RecordHeaderError](err); ok {
		return true
	}

	if _, ok := errors.AsType[tls.AlertError](err); ok {
		return true
	}

	if _, ok := errors.AsType[x509.UnknownAuthorityError](err); ok {
		return true
	}

	if _, ok := errors.AsType[x509.HostnameError](err); ok {
		return true
	}

	_, ok := errors.AsType[x509.CertificateInvalidError](err)

	return ok
}
