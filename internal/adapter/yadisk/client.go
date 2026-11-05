// Package yadisk is the Yandex Disk adapter: the object store over the Disk
// REST API (spec §5.4, §5.5) and the OAuth sign-in.
package yadisk

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
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
// a known Yandex error name or "other". The text of the answer and the URLs
// of the request never get into it (spec §5.4, §9).
type CloudError struct {
	Status int
	Code   string

	err error // why there is no answer, without the request URL
}

func (e CloudError) Error() string { return fmt.Sprintf("yandex disk error: %d %s", e.Status, e.Code) }

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

	return CloudError{Code: codeNetwork, err: err}
}

// isStatus reports whether err is an answer with status.
func isStatus(err error, status int) bool {
	ce, ok := errors.AsType[CloudError](err)
	return ok && ce.Status == status
}

// defaultHTTP is the base client of both kinds of requests: system roots,
// TLS 1.2 or later and the proxy from the environment. One transport serves
// all stores, so connections are reused between operations.
var defaultHTTP = sync.OnceValue(func() *http.Client {
	t := &http.Transport{Proxy: http.ProxyFromEnvironment}
	if dt, ok := http.DefaultTransport.(*http.Transport); ok {
		t = dt.Clone()
	}

	t.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}

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

// send sends the request with the access token and decodes a 2xx answer into
// out (nil skips the body). It returns the status of the answer.
func (c apiClient) send(ctx context.Context, method string, u *url.URL, access string, out any) (int, error) {
	if c.base == nil || u.Scheme != schemeHTTPS || u.Host != c.base.Host {
		return 0, errForeignHost
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), http.NoBody)
	if err != nil {
		return 0, fmt.Errorf("build %s request: %w", method, errBadLink)
	}

	req.Header.Set("Authorization", "OAuth "+access)
	req.Header.Set("Accept", "application/json")

	return roundTrip(c.http, req, out)
}

// roundTrip sends req and decodes a 2xx JSON answer into out (nil skips it);
// any other status becomes a CloudError.
func roundTrip(hc *http.Client, req *http.Request, out any) (int, error) {
	resp, err := hc.Do(req)
	if err != nil {
		return 0, networkError(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	if err != nil {
		return 0, networkError(err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return resp.StatusCode, answerError(resp.StatusCode, body)
	}

	if out != nil && len(body) > 0 {
		if err := json.Unmarshal(body, out); err != nil {
			return resp.StatusCode, CloudError{Status: resp.StatusCode, Code: codeOther}
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

// secureLink parses a link of an answer and requires https.
func secureLink(href string) (string, error) {
	u, err := url.Parse(href)
	if err != nil {
		return "", CloudError{Code: codeOther, err: errBadLink}
	}

	if u.Scheme != schemeHTTPS || u.Host == "" {
		return "", CloudError{Code: codeOther, err: errInsecureLink}
	}

	return u.String(), nil
}

// get downloads href; more than limit bytes is errTooLarge.
func (c hrefClient) get(ctx context.Context, href string, limit int64) ([]byte, error) {
	link, err := secureLink(href)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, http.NoBody)
	if err != nil {
		return nil, CloudError{Code: codeOther, err: errBadLink}
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, networkError(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxAnswer)) // the status is the error
		return nil, answerError(resp.StatusCode, body)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, networkError(err)
	}

	if int64(len(data)) > limit {
		return nil, errTooLarge
	}

	return data, nil
}

// put uploads data to href.
func (c hrefClient) put(ctx context.Context, href string, data []byte) error {
	link, err := secureLink(href)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, link, bytes.NewReader(data))
	if err != nil {
		return CloudError{Code: codeOther, err: errBadLink}
	}

	_, err = roundTrip(c.http, req, nil)

	return err
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
		return tokenAnswer{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, link, strings.NewReader(form.Encode()))
	if err != nil {
		return tokenAnswer{}, CloudError{Code: codeOther, err: errBadLink}
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	var answer tokenAnswer
	if _, err := roundTrip(hc, req, &answer); err != nil {
		return tokenAnswer{}, err
	}

	if answer.AccessToken == "" {
		return tokenAnswer{}, CloudError{Status: http.StatusOK, Code: codeOther, err: errNoAccess}
	}

	return answer, nil
}
