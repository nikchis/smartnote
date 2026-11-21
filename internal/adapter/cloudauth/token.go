package cloudauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/nikchis/smartnote/internal/adapter/cloudhttp"
)

const (
	// codeOther is the Code of an answer whose error is not a known name.
	codeOther = "other"

	maxAnswer    = 1 << 20          // the largest JSON answer read
	maxExpiresIn = 10 * 365 * 86400 // seconds; a larger expires_in is capped

	// OAuth errors of a refused grant or client.
	codeInvalidGrant       = "invalid_grant"
	codeInvalidClient      = "invalid_client"
	codeUnauthorizedClient = "unauthorized_client"
)

// knownCodes are the error names of OAuth that CloudError keeps; any other
// name becomes codeOther.
var knownCodes = map[string]bool{
	"invalid_request":           true,
	codeInvalidGrant:            true,
	codeInvalidClient:           true,
	codeUnauthorizedClient:      true,
	"unsupported_grant_type":    true,
	"invalid_scope":             true,
	errorAccessDenied:           true,
	"bad_verification_code":     true,
	"unsupported_response_type": true,
	"server_error":              true,
	"temporarily_unavailable":   true,
}

var (
	errInsecureLink = errors.New("link is not https")
	errBadLink      = errors.New("link cannot be parsed")
	errNoAccess     = errors.New("token answer without access_token")
	errNoRefresh    = errors.New("token answer without refresh_token")
)

func knownCode(name string) string {
	if knownCodes[name] {
		return name
	}

	return codeOther
}

// answerError is the error of an answer of the provider with status and no
// known error name; cause must not quote a URL.
func (a *Authorizer) answerError(status int, cause error) cloudhttp.CloudError {
	return cloudhttp.WithCause(cloudhttp.NewAnswerError(a.p.Kind, status, codeOther, cloudhttp.ReasonNone), cause)
}

// tokenAnswer is a successful answer of the OAuth token endpoint.
type tokenAnswer struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

// expiry is the end of the token's life counted from now; zero when the
// answer gives none.
func (a tokenAnswer) expiry(now time.Time) time.Time {
	if a.ExpiresIn <= 0 {
		return time.Time{}
	}

	return now.Add(time.Duration(min(a.ExpiresIn, maxExpiresIn)) * time.Second)
}

// postToken sends form to the token endpoint within exchangeTimeout, only
// over https. An error answer is a CloudError with the OAuth error name; so
// is an answer without access_token. When ctx ends first the error is
// ctx's, wrapped with what, not a network one.
func (a *Authorizer) postToken(ctx context.Context, form url.Values, what string) (tokenAnswer, error) {
	opCtx, cancel := context.WithTimeout(ctx, exchangeTimeout)
	defer cancel()

	answer, err := a.send(opCtx, form)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return tokenAnswer{}, fmt.Errorf("%s: %w", what, ctxErr)
		}

		return tokenAnswer{}, fmt.Errorf("%s: %w", what, err)
	}

	return answer, nil
}

func (a *Authorizer) send(ctx context.Context, form url.Values) (tokenAnswer, error) {
	link, err := httpsLink(a.p.TokenURL)
	if err != nil {
		return tokenAnswer{}, a.answerError(0, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, link, strings.NewReader(form.Encode()))
	if err != nil {
		return tokenAnswer{}, a.answerError(0, errBadLink)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := a.http.Do(req)
	if err != nil {
		return tokenAnswer{}, cloudhttp.RequestError(ctx, a.p.Kind, err, "")
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	if err != nil {
		return tokenAnswer{}, cloudhttp.RequestError(ctx, a.p.Kind, err, "")
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var b struct {
			Error string `json:"error"`
		}

		_ = json.Unmarshal(body, &b) // a body that is not JSON keeps an empty name

		return tokenAnswer{}, cloudhttp.NewAnswerError(a.p.Kind, resp.StatusCode, knownCode(b.Error), cloudhttp.ReasonNone)
	}

	var answer tokenAnswer
	if len(body) > 0 {
		if err := json.Unmarshal(body, &answer); err != nil {
			return tokenAnswer{}, a.answerError(resp.StatusCode, nil)
		}
	}

	if answer.AccessToken == "" {
		return tokenAnswer{}, a.answerError(http.StatusOK, errNoAccess)
	}

	return answer, nil
}

// httpsLink parses an endpoint of the OAuth server and requires https. Its
// errors are errBadLink and errInsecureLink.
func httpsLink(href string) (string, error) {
	u, err := url.Parse(href)
	if err != nil {
		return "", errBadLink // the parse error quotes the link
	}

	if u.Scheme != "https" || u.Host == "" {
		return "", errInsecureLink
	}

	return u.String(), nil
}
