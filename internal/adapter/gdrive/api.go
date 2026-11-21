package gdrive

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/nikchis/smartnote/internal/adapter/cloudhttp"
	"github.com/nikchis/smartnote/internal/domain"
)

const (
	// codeOther is the Code of an answer whose reason is not a known name.
	codeOther = "other"

	maxAnswer   = 1 << 20 // the largest JSON answer read
	schemeHTTPS = "https"

	// The hosts that get the access token (spec §5): the API and its uploads,
	// and the token endpoint.
	apiHost   = "www.googleapis.com"
	oauthHost = "oauth2.googleapis.com"
)

// Steps of the Store, CloudError.Step: the request that failed.
const (
	stepList     = "list"     // files.list of the object name
	stepDownload = "download" // files.get with alt=media
)

// Reasons of Drive error answers that have a cause of §9.
const (
	reasonStorageQuota  = "storageQuotaExceeded"
	reasonUserRateLimit = "userRateLimitExceeded"
	reasonRateLimit     = "rateLimitExceeded"
)

// knownReasons are the reasons of Drive error answers that CloudError keeps
// (spec §5.7); any other becomes codeOther.
var knownReasons = map[string]bool{
	reasonStorageQuota:            true,
	reasonUserRateLimit:           true,
	reasonRateLimit:               true,
	"dailyLimitExceeded":          true,
	"notFound":                    true,
	"insufficientPermissions":     true,
	"insufficientFilePermissions": true,
	"appNotAuthorizedToFile":      true,
	"authError":                   true,
	"backendError":                true,
	"internalError":               true,
	"badRequest":                  true,
	"invalid":                     true,
	"invalidParameter":            true,
	"required":                    true,
	"clientClosedRequest":         true,
}

var (
	errForeignHost = errors.New("request outside the Google API hosts")
	errBadAnswer   = errors.New("answer cannot be decoded")
	errTooLarge    = errors.New("object exceeds the size limit")
)

// googleHost reports whether hostname, lowercased and without port, is a
// host of Google that gets the access token.
func googleHost(hostname string) bool {
	return hostname == apiHost || hostname == oauthHost
}

// bearerAllowed reports whether a request to u may carry the access token:
// u is https and allow accepts its host, compared in lower case and without
// the port.
func bearerAllowed(u *url.URL, allow func(string) bool) bool {
	if u.Scheme != schemeHTTPS || u.User != nil {
		return false
	}

	host := strings.ToLower(u.Hostname())

	return host != "" && allow(host)
}

// answerError reads the reason of a Drive error answer
// {"error": {"errors": [{"reason"}]}}: the first one of the closed list
// becomes the Code. A body that is not of this form gives codeOther. The
// message of the answer is not kept.
func answerError(status int, body []byte) cloudhttp.CloudError {
	var b struct {
		Error struct {
			Errors []struct {
				Reason string `json:"reason"`
			} `json:"errors"`
		} `json:"error"`
	}

	_ = json.Unmarshal(body, &b) // a body that is not JSON keeps no reason

	code := codeOther

	for _, e := range b.Error.Errors {
		if knownReasons[e.Reason] {
			code = e.Reason
			break
		}
	}

	return cloudhttp.NewAnswerError(domain.KindGoogleDrive, status, code, reasonOf(status, code))
}

// reasonOf is the known cause of an answer (spec §9): a full Drive, or a
// rate limit.
func reasonOf(status int, code string) cloudhttp.Reason {
	switch {
	case status == http.StatusTooManyRequests:
		return cloudhttp.ReasonBusy
	case status != http.StatusForbidden:
		return cloudhttp.ReasonNone
	case code == reasonStorageQuota:
		return cloudhttp.ReasonNoSpace
	case code == reasonUserRateLimit || code == reasonRateLimit:
		return cloudhttp.ReasonBusy
	default:
		return cloudhttp.ReasonNone
	}
}

// otherError is the error of a failure without a known reason at step, with
// its cause, which must not quote a URL.
func otherError(status int, step string, cause error) cloudhttp.CloudError {
	ce := cloudhttp.NewAnswerError(domain.KindGoogleDrive, status, codeOther, cloudhttp.ReasonNone)

	return cloudhttp.WithCause(cloudhttp.WithStep(ce, step), cause)
}

// apiClient sends the requests with the access token, only over https and
// only to the hosts allow accepts. It does not follow redirects, so the
// token never leaves those hosts.
type apiClient struct {
	http  *http.Client
	api   string // Endpoints.API without the trailing slash
	allow func(hostname string) bool
}

// newAPIClient returns a client over base, or the default client, for the
// API at api. A nil allow takes googleHost.
func newAPIClient(base *http.Client, api string, allow func(string) bool) apiClient {
	if allow == nil {
		allow = googleHost
	}

	return apiClient{
		http:  cloudhttp.WithRedirects(base, func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }),
		api:   strings.TrimSuffix(api, "/"),
		allow: allow,
	}
}

// url returns the API URL of path, whose parts must already be escaped,
// with query q.
func (c apiClient) url(path string, q url.Values) (*url.URL, error) {
	u, err := url.Parse(c.api + path)
	if err != nil {
		return nil, errForeignHost // the parse error quotes the URL
	}

	u.RawQuery = q.Encode()

	return u, nil
}

// send sends a GET of step to u with the access token. It returns the
// answer of a 2xx status, whose body the caller closes; any other status is
// a CloudError.
func (c apiClient) send(ctx context.Context, step string, u *url.URL, access string) (*http.Response, error) {
	if !bearerAllowed(u, c.allow) {
		return nil, otherError(0, step, errForeignHost)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), http.NoBody)
	if err != nil {
		return nil, otherError(0, step, errForeignHost)
	}

	req.Header.Set("Authorization", "Bearer "+access)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, cloudhttp.RequestError(ctx, domain.KindGoogleDrive, err, step)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		defer resp.Body.Close()

		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxAnswer)) // the status is the error

		return nil, cloudhttp.WithStep(answerError(resp.StatusCode, body), step)
	}

	return resp, nil
}

// getJSON sends a GET of step to u and decodes the JSON answer into out.
func (c apiClient) getJSON(ctx context.Context, step string, u *url.URL, access string, out any) error {
	resp, err := c.send(ctx, step, u, access)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	if err != nil {
		return cloudhttp.RequestError(ctx, domain.KindGoogleDrive, err, step)
	}

	if err := json.Unmarshal(body, out); err != nil {
		return otherError(resp.StatusCode, step, errBadAnswer)
	}

	return nil
}

// getMedia sends a GET of step to u and returns the body, counting its
// bytes with m; more than limit bytes is errTooLarge.
func (c apiClient) getMedia(ctx context.Context, step string, u *url.URL, access string, limit int64, m *cloudhttp.Meter) ([]byte, error) {
	defer m.Stop()

	resp, err := c.send(ctx, step, u, access)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(m.Wrap(resp.Body), limit+1))
	if err != nil {
		return nil, cloudhttp.RequestError(ctx, domain.KindGoogleDrive, err, step)
	}

	if int64(len(data)) > limit {
		return nil, errTooLarge
	}

	return data, nil
}
