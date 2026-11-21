// Package yadisk is the Yandex Disk adapter: the object store over the Disk
// REST API (spec §5.4, §5.5) and the OAuth sign-in.
package yadisk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/nikchis/smartnote/internal/adapter/cloudhttp"
	"github.com/nikchis/smartnote/internal/domain"
)

// Endpoints are the base URLs of the Disk API and of Yandex OAuth.
type Endpoints struct {
	API, OAuth string
}

// DefaultEndpoints are the production endpoints.
var DefaultEndpoints = Endpoints{API: "https://cloud-api.yandex.net", OAuth: "https://oauth.yandex.ru"}

const (
	// codeOther is the Code of an answer whose error is not a known name.
	codeOther = "other"

	maxAnswer    = 1 << 20 // the largest JSON answer read
	maxRedirects = 10

	schemeHTTPS = "https"

	// OAuth errors of a refused grant or client.
	codeInvalidGrant       = "invalid_grant"
	codeInvalidClient      = "invalid_client"
	codeUnauthorizedClient = "unauthorized_client"
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
)

func knownCode(name string) string {
	if knownCodes[name] {
		return name
	}

	return codeOther
}

// answerError reads the error name of an answer: the Disk API and OAuth both
// put it under "error"; a body that is not JSON gives codeOther. The reason
// comes from the status, as the Disk API gives no other sign of it.
func answerError(status int, body []byte) cloudhttp.CloudError {
	var b struct {
		Error string `json:"error"`
	}

	_ = json.Unmarshal(body, &b) // a body that is not JSON keeps an empty name

	return cloudhttp.NewAnswerError(domain.KindYandexDisk, status, knownCode(b.Error), statusReason(status))
}

// statusReason is the known cause of an answer status: 507 is a full disk,
// 413 a file over the plan's limit.
func statusReason(status int) cloudhttp.Reason {
	switch status {
	case http.StatusInsufficientStorage:
		return cloudhttp.ReasonNoSpace
	case http.StatusRequestEntityTooLarge:
		return cloudhttp.ReasonTooLarge
	default:
		return cloudhttp.ReasonNone
	}
}

// otherError is the error of a failure without a known name, with its cause
// (which must not quote a URL) and the step of the Store, if any.
func otherError(status int, step string, cause error) cloudhttp.CloudError {
	ce := cloudhttp.NewAnswerError(domain.KindYandexDisk, status, codeOther, statusReason(status))

	return cloudhttp.WithCause(cloudhttp.WithStep(ce, step), cause)
}

// networkError is a request without an answer.
func networkError(err error) cloudhttp.CloudError {
	return cloudhttp.NewNetworkError(domain.KindYandexDisk, err)
}

// requestError is the error of a request of step that got no answer.
func requestError(ctx context.Context, err error, step string) error {
	return cloudhttp.RequestError(ctx, domain.KindYandexDisk, err, step)
}

// apiClient sends requests with the OAuth token, only over https and only to
// the host of Endpoints.API. It does not follow redirects, so the token never
// leaves that host.
type apiClient struct {
	http *http.Client
	base *url.URL // nil when Endpoints.API is not an https URL
}

func newAPIClient(base *http.Client, api string) apiClient {
	c := apiClient{http: cloudhttp.WithRedirects(base, func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse })}
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
	fail := func(ce cloudhttp.CloudError) (int, error) {
		ce.Step = step
		return ce.Status, ce
	}

	resp, err := hc.Do(req)
	if err != nil {
		return 0, requestError(req.Context(), err, step)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	if err != nil {
		return 0, requestError(req.Context(), err, step)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fail(answerError(resp.StatusCode, body))
	}

	if out != nil && len(body) > 0 {
		if err := json.Unmarshal(body, out); err != nil {
			return fail(otherError(resp.StatusCode, "", nil))
		}
	}

	return resp.StatusCode, nil
}

// hrefClient uploads and downloads by the links of the API. It never sets
// Authorization, follows redirects only to https and drops Authorization on
// each redirect in case a base client's transport adds one.
type hrefClient struct {
	http  *http.Client
	allow func(hostname string) bool // the hosts a link may point to
}

// newHrefClient returns a client for operation links. allow decides on a
// lowercased hostname without port; nil takes yandexLinkHost.
func newHrefClient(base *http.Client, allow func(string) bool) hrefClient {
	if allow == nil {
		allow = yandexLinkHost
	}

	return hrefClient{
		http: cloudhttp.WithRedirects(base, func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return errRedirects
			}

			if !linkAllowed(req.URL, allow) {
				return errInsecureLink
			}

			req.Header.Del("Authorization")

			return nil
		}),
		allow: allow,
	}
}

// yandexLinkHost reports whether hostname is yandex.ru or yandex.net or a
// subdomain of either, at any depth. A name that only ends with these
// letters, such as evilyandex.net, is not.
func yandexLinkHost(hostname string) bool {
	for _, domain := range [...]string{"yandex.ru", "yandex.net"} {
		if hostname == domain || strings.HasSuffix(hostname, "."+domain) {
			return true
		}
	}

	return false
}

// linkAllowed reports whether u is an https URL to a host that allow
// accepts; the host is compared in lower case and without the port.
func linkAllowed(u *url.URL, allow func(string) bool) bool {
	if u.Scheme != schemeHTTPS {
		return false
	}

	host := strings.ToLower(u.Hostname())

	return host != "" && allow(host)
}

// secureLink parses a link of an answer and requires https and a host that
// c.allow accepts. Its errors are errBadLink and errInsecureLink and carry
// no part of the link.
func (c hrefClient) secureLink(href string) (string, error) {
	u, err := url.Parse(href)
	if err != nil {
		return "", errBadLink // the parse error quotes the link
	}

	if !linkAllowed(u, c.allow) {
		return "", errInsecureLink
	}

	return u.String(), nil
}

// get downloads href, counting the bytes of the body with m; more than limit
// bytes is errTooLarge.
func (c hrefClient) get(ctx context.Context, href string, limit int64, m *cloudhttp.Meter) ([]byte, error) {
	defer m.Stop()

	link, err := c.secureLink(href)
	if err != nil {
		return nil, otherError(0, stepDownload, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, http.NoBody)
	if err != nil {
		return nil, otherError(0, stepDownload, errBadLink)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, requestError(ctx, err, stepDownload)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxAnswer)) // the status is the error
		return nil, cloudhttp.WithStep(answerError(resp.StatusCode, body), stepDownload)
	}

	data, err := io.ReadAll(io.LimitReader(m.Wrap(resp.Body), limit+1))
	if err != nil {
		return nil, requestError(ctx, err, stepDownload)
	}

	if int64(len(data)) > limit {
		return nil, errTooLarge
	}

	return data, nil
}

// put uploads data to href, counting the bytes of the body with m.
func (c hrefClient) put(ctx context.Context, href string, data []byte, m *cloudhttp.Meter) error {
	defer m.Stop() // the transport may read the body after Do returns

	link, err := c.secureLink(href)
	if err != nil {
		return otherError(0, stepUpload, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, link, m.Wrap(bytes.NewReader(data)))
	if err != nil {
		return otherError(0, stepUpload, errBadLink)
	}

	// The body is not a *bytes.Reader any more: its length and a fresh copy
	// for a retry or a redirect are set here, as NewRequest would.
	req.ContentLength = int64(len(data))
	req.GetBody = func() (io.ReadCloser, error) {
		m.Restart()
		return io.NopCloser(m.Wrap(bytes.NewReader(data))), nil
	}

	if len(data) == 0 {
		req.Body, req.GetBody = http.NoBody, nil
	}

	_, err = roundTrip(c.http, req, stepUpload, nil)

	return err
}
