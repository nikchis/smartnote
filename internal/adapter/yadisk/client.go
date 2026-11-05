// Package yadisk is the Yandex Disk adapter: the object store over the Disk
// REST API (spec §5.4, §5.5) and the OAuth sign-in.
package yadisk

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Endpoints are the base URLs of the Disk API and of Yandex OAuth.
type Endpoints struct {
	API, OAuth string
}

// DefaultEndpoints are the production endpoints.
var DefaultEndpoints = Endpoints{API: "https://cloud-api.yandex.net", OAuth: "https://oauth.yandex.ru"}

const (
	// codeNetwork is the Code of a request that got no answer.
	codeNetwork = "network"
	// codeOther is the Code of an answer whose error is not a known name.
	codeOther = "other"

	maxAnswer    = 1 << 20 // the largest JSON answer read
	maxRedirects = 10

	schemeHTTPS = "https"

	maxExpiresIn = 10 * 365 * 86400 // seconds; a larger expires_in is capped

	// Parameters of the OAuth requests.
	paramGrantType = "grant_type"
	paramClientID  = "client_id"
	paramCode      = "code"

	// OAuth errors of a refused grant or client.
	codeInvalidGrant       = "invalid_grant"
	codeInvalidClient      = "invalid_client"
	codeUnauthorizedClient = "unauthorized_client"

	// idleConnTimeout closes a kept connection that no request used for this
	// long: a long-idle connection may already be dropped by the network.
	idleConnTimeout = 30 * time.Second
)

// Steps of the Store, CloudError.Step: the request that failed.
const (
	stepStat         = "stat"          // metadata of the object, before the download and the move
	stepDownloadLink = "download_link" // GET /resources/download
	stepDownload     = "download"      // GET of the download link
	stepUploadLink   = "upload_link"   // GET /resources/upload
	stepUpload       = "upload"        // PUT to the upload link
	stepWaitMD5      = "wait_md5"      // metadata of the uploaded file
	stepMove         = "move"          // the move and its operation
	stepFence        = "fence"         // the fence of a move with an unknown outcome
	stepCleanup      = "cleanup"       // the cleanup of old temporary files
	stepDelete       = "delete"        // deletion of the Write's own temporary file
)

// Classes of a network error, CloudError.Net.
const (
	netTimeout = "timeout"
	netReset   = "reset"
	netEOF     = "eof"
	netDNS     = "dns"
	netTLS     = "tls"
	netRefused = "refused"
	netOther   = "other"
)

// knownCodes are the error names of the Disk API and of Yandex OAuth that
// CloudError keeps; any other name becomes codeOther.
var knownCodes = map[string]bool{
	"UnauthorizedError":              true,
	"ForbiddenError":                 true,
	"DiskNotFoundError":              true,
	"DiskPathDoesntExistsError":      true,
	"DiskResourceAlreadyExistsError": true,
	"DiskStorageQuotaExhaustedError": true,
	"FieldValidationError":           true,
	"TooManyRequestsError":           true,
	"DiskInternalError":              true,
	"LockedError":                    true,
	"ResourceLockedError":            true,
	"invalid_request":                true,
	codeInvalidGrant:                 true,
	codeInvalidClient:                true,
	codeUnauthorizedClient:           true,
	"unsupported_grant_type":         true,
	"invalid_scope":                  true,
	"access_denied":                  true,
	"bad_verification_code":          true,
	"unsupported_response_type":      true,
	"server_error":                   true,
	"temporarily_unavailable":        true,
}

var (
	errInsecureLink = errors.New("link is not https")
	errForeignHost  = errors.New("request outside the API host")
	errBadLink      = errors.New("link cannot be parsed")
	errRedirects    = errors.New("too many redirects")
	errTooLarge     = errors.New("object exceeds the size limit")
	errNoAccess     = errors.New("token answer without access_token")
)

// CloudError is an error answer of Yandex Disk or OAuth (Status is the HTTP
// status) or a request without an answer (Status 0, Code "network"). Code is
// a known Yandex error name or "other". Step names the request of the Store
// that failed ("" for OAuth), Net the class of a network error ("timeout",
// "reset", "eof", "dns", "tls", "refused" or "other"). The text of the answer
// and the URLs of the request never get into it (spec §5.4, §9).
type CloudError struct {
	Status int
	Code   string
	Step   string
	Net    string

	err error // why there is no answer, without the request URL
}

func (e CloudError) Error() string {
	text := fmt.Sprintf("yandex disk error: %d %s", e.Status, e.Code)
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

func knownCode(name string) string {
	if knownCodes[name] {
		return name
	}

	return codeOther
}

// answerError reads the error name of an answer: the Disk API and OAuth both
// put it under "error"; a body that is not JSON gives codeOther.
func answerError(status int, body []byte) CloudError {
	var b struct {
		Error string `json:"error"`
	}

	_ = json.Unmarshal(body, &b) // a body that is not JSON keeps an empty name

	return CloudError{Status: status, Code: knownCode(b.Error)}
}

// networkError is a request without an answer. The *url.Error is dropped:
// its text has the request URL, which carries the path and, for a link, its
// signed parameters.
func networkError(err error) CloudError {
	if ue, ok := errors.AsType[*url.Error](err); ok {
		err = ue.Err
	}

	return CloudError{Code: codeNetwork, Net: netClass(err), err: err}
}

// netClass is the class of the cause of a request without an answer, for
// the log: the cause itself may quote addresses.
func netClass(err error) string {
	switch {
	case isDNS(err):
		return netDNS
	case isTLS(err):
		return netTLS
	case isTimeout(err):
		return netTimeout
	case errors.Is(err, syscall.ECONNRESET):
		return netReset
	case errors.Is(err, syscall.ECONNREFUSED):
		return netRefused
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return netEOF
	default:
		return netOther
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

// isStatus reports whether err is an answer with status.
func isStatus(err error, status int) bool {
	ce, ok := errors.AsType[CloudError](err)
	return ok && ce.Status == status
}

// defaultHTTP is the base client of both kinds of requests: system roots,
// TLS 1.2 or later and the proxy from the environment. One transport serves
// all stores, so connections are reused within an operation; Store.Close
// closes the idle ones at its end, and idleConnTimeout bounds their life.
var defaultHTTP = sync.OnceValue(func() *http.Client {
	t := &http.Transport{Proxy: http.ProxyFromEnvironment}
	if dt, ok := http.DefaultTransport.(*http.Transport); ok {
		t = dt.Clone()
	}

	t.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	t.IdleConnTimeout = idleConnTimeout

	return &http.Client{Transport: t}
})

// withRedirects returns a copy of base, or of the default client, with the
// redirect policy check.
func withRedirects(base *http.Client, check func(*http.Request, []*http.Request) error) *http.Client {
	if base == nil {
		base = defaultHTTP()
	}

	c := *base
	c.CheckRedirect = check

	return &c
}

// apiClient sends requests with the OAuth token, only over https and only to
// the host of Endpoints.API. It does not follow redirects, so the token never
// leaves that host.
type apiClient struct {
	http *http.Client
	base *url.URL // nil when Endpoints.API is not an https URL
}

func newAPIClient(base *http.Client, api string) apiClient {
	c := apiClient{http: withRedirects(base, func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse })}
	if u, err := url.Parse(api); err == nil && u.Scheme == schemeHTTPS && u.Host != "" {
		c.base = u
	}

	return c
}

// url returns the API URL of path with query q.
func (c apiClient) url(path string, q url.Values) (*url.URL, error) {
	if c.base == nil {
		return nil, errForeignHost
	}

	u := c.base.JoinPath(path)
	u.RawQuery = q.Encode()

	return u, nil
}

// link parses an operation link of an answer; send checks its host.
func (c apiClient) link(href string) (*url.URL, error) {
	u, err := url.Parse(href)
	if err != nil {
		return nil, errBadLink // the parse error quotes the link
	}

	return u, nil
}

// send sends the request of step with the access token and decodes a 2xx
// answer into out (nil skips the body). It returns the status of the answer.
func (c apiClient) send(ctx context.Context, step, method string, u *url.URL, access string, out any) (int, error) {
	if c.base == nil || u.Scheme != schemeHTTPS || u.Host != c.base.Host {
		return 0, errForeignHost
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), http.NoBody)
	if err != nil {
		return 0, fmt.Errorf("build %s request: %w", method, errBadLink)
	}

	req.Header.Set("Authorization", "OAuth "+access)
	req.Header.Set("Accept", "application/json")

	return roundTrip(c.http, req, step, out)
}

// roundTrip sends req, the request of step, and decodes a 2xx JSON answer
// into out (nil skips it); any other status becomes a CloudError.
func roundTrip(hc *http.Client, req *http.Request, step string, out any) (int, error) {
	fail := func(ce CloudError) (int, error) {
		ce.Step = step
		return ce.Status, ce
	}

	resp, err := hc.Do(req)
	if err != nil {
		return fail(networkError(err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	if err != nil {
		return fail(networkError(err))
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fail(answerError(resp.StatusCode, body))
	}

	if out != nil && len(body) > 0 {
		if err := json.Unmarshal(body, out); err != nil {
			return fail(CloudError{Status: resp.StatusCode, Code: codeOther})
		}
	}

	return resp.StatusCode, nil
}

// hrefClient uploads and downloads by the links of the API. It never sets
// Authorization, follows redirects only to https and drops Authorization on
// each redirect in case a base client's transport adds one.
type hrefClient struct {
	http *http.Client
}

func newHrefClient(base *http.Client) hrefClient {
	return hrefClient{http: withRedirects(base, checkHrefRedirect)}
}

func checkHrefRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return errRedirects
	}

	if req.URL.Scheme != schemeHTTPS {
		return errInsecureLink
	}

	req.Header.Del("Authorization")

	return nil
}

// secureLink parses a link of an answer and requires https. Its errors are
// errBadLink and errInsecureLink.
func secureLink(href string) (string, error) {
	u, err := url.Parse(href)
	if err != nil {
		return "", errBadLink // the parse error quotes the link
	}

	if u.Scheme != schemeHTTPS || u.Host == "" {
		return "", errInsecureLink
	}

	return u.String(), nil
}

// get downloads href, counting the bytes of the body with m; more than limit
// bytes is errTooLarge.
func (c hrefClient) get(ctx context.Context, href string, limit int64, m *meter) ([]byte, error) {
	defer m.stop()

	link, err := secureLink(href)
	if err != nil {
		return nil, CloudError{Code: codeOther, Step: stepDownload, err: err}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, http.NoBody)
	if err != nil {
		return nil, CloudError{Code: codeOther, Step: stepDownload, err: errBadLink}
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, withStep(networkError(err), stepDownload)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxAnswer)) // the status is the error
		return nil, withStep(answerError(resp.StatusCode, body), stepDownload)
	}

	data, err := io.ReadAll(io.LimitReader(meteredReader{r: resp.Body, m: m}, limit+1))
	if err != nil {
		return nil, withStep(networkError(err), stepDownload)
	}

	if int64(len(data)) > limit {
		return nil, errTooLarge
	}

	return data, nil
}

// put uploads data to href, counting the bytes of the body with m.
func (c hrefClient) put(ctx context.Context, href string, data []byte, m *meter) error {
	defer m.stop() // the transport may read the body after Do returns

	link, err := secureLink(href)
	if err != nil {
		return CloudError{Code: codeOther, Step: stepUpload, err: err}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, link, meteredReader{r: bytes.NewReader(data), m: m})
	if err != nil {
		return CloudError{Code: codeOther, Step: stepUpload, err: errBadLink}
	}

	// The body is not a *bytes.Reader any more: its length and a fresh copy
	// for a retry or a redirect are set here, as NewRequest would.
	req.ContentLength = int64(len(data))
	req.GetBody = func() (io.ReadCloser, error) {
		m.restart()
		return io.NopCloser(meteredReader{r: bytes.NewReader(data), m: m}), nil
	}

	if len(data) == 0 {
		req.Body, req.GetBody = http.NoBody, nil
	}

	_, err = roundTrip(c.http, req, stepUpload, nil)

	return err
}

// withStep returns ce marked with step.
func withStep(ce CloudError, step string) CloudError {
	ce.Step = step
	return ce
}

// meter counts the bytes of one transfer and reports the count. After stop
// it reports nothing: the transport may read a request body from its own
// goroutine even after the request is over. A nil meter counts nothing.
type meter struct {
	report func(done int64)

	mu      sync.Mutex
	done    int64
	stopped bool
}

// startMeter reports 0 to report and returns a meter that reports to it.
func startMeter(report func(done int64)) *meter {
	report(0)
	return &meter{report: report}
}

func (m *meter) add(n int) {
	if m == nil || n <= 0 {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.stopped {
		return
	}

	m.done += int64(n)
	m.report(m.done)
}

// restart counts from 0 again: the body is sent anew.
func (m *meter) restart() {
	if m == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.done = 0
}

func (m *meter) stop() {
	if m == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.stopped = true
}

// meteredReader counts what is read from r with m.
type meteredReader struct {
	r io.Reader
	m *meter
}

func (r meteredReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	r.m.add(n)

	return n, err
}

// tokenAnswer is a successful answer of the OAuth token endpoint.
type tokenAnswer struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
}

// expiry is the end of the token's life counted from now; zero when the
// answer gives none.
func (a tokenAnswer) expiry(now time.Time) time.Time {
	if a.ExpiresIn <= 0 {
		return time.Time{}
	}

	return now.Add(time.Duration(min(a.ExpiresIn, maxExpiresIn)) * time.Second)
}

// postToken sends form to the OAuth token endpoint, only over https, with hc,
// which must not follow redirects. An error answer is a CloudError with the
// OAuth error name; so is an answer without access_token.
func postToken(ctx context.Context, hc *http.Client, endpoint string, form url.Values) (tokenAnswer, error) {
	link, err := secureLink(endpoint)
	if err != nil {
		return tokenAnswer{}, CloudError{Code: codeOther, err: err}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, link, strings.NewReader(form.Encode()))
	if err != nil {
		return tokenAnswer{}, CloudError{Code: codeOther, err: errBadLink}
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	var answer tokenAnswer
	if _, err := roundTrip(hc, req, "", &answer); err != nil {
		return tokenAnswer{}, err
	}

	if answer.AccessToken == "" {
		return tokenAnswer{}, CloudError{Status: http.StatusOK, Code: codeOther, err: errNoAccess}
	}

	return answer, nil
}
