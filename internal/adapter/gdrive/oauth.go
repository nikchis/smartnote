package gdrive

import (
	"net/url"
	"strings"

	"github.com/nikchis/smartnote/internal/adapter/cloudauth"
	"github.com/nikchis/smartnote/internal/domain"
)

const (
	// authorizeURL is the sign-in page of Google; it is not an API endpoint.
	authorizeURL = "https://accounts.google.com/o/oauth2/v2/auth"

	// scopeAppData is the only permission Smartnote asks for: the hidden
	// app folder of the Drive (spec §1.1).
	scopeAppData = "https://www.googleapis.com/auth/drive.appdata"

	// loopbackAddr takes a free port: Google accepts any loopback port of a
	// Desktop client (spec §6).
	loopbackAddr = "127.0.0.1:0"
)

// OAuthProvider returns the sign-in parameters of Google (spec §6, П1, П2):
// the receiver on a random loopback port with /callback, the drive.appdata
// scope and offline access for a refresh token. prompt=consent is not sent:
// Google gives a refresh token on a repeated sign-in without it. There is
// no sign-in with a code to copy.
func OAuthProvider() cloudauth.Provider {
	return oauthProvider(DefaultEndpoints)
}

// oauthProvider is OAuthProvider over the token endpoint of e.
func oauthProvider(e Endpoints) cloudauth.Provider {
	return cloudauth.Provider{
		Kind:         domain.KindGoogleDrive,
		Name:         "Google Drive",
		Issuer:       "Google",
		AuthorizeURL: authorizeURL,
		TokenURL:     strings.TrimSuffix(e.OAuth, "/") + "/token",
		Scope:        scopeAppData,
		LoopbackAddr: loopbackAddr,
		CallbackPath: "/callback",
		Extra:        url.Values{"access_type": {"offline"}},
	}
}
