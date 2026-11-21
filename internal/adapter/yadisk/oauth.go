package yadisk

import (
	"strings"

	"github.com/nikchis/smartnote/internal/adapter/cloudauth"
	"github.com/nikchis/smartnote/internal/domain"
)

// manualRedirect is the page of Yandex that shows the code to copy.
const manualRedirect = "https://oauth.yandex.ru/verification_code"

// OAuthProvider returns the sign-in parameters of Yandex ID (spec §6.2,
// §6.3): the receiver on the fixed address that is registered with the app,
// and the page that shows the code. Yandex has no scope in the request, no
// client secret and no refresh token.
func OAuthProvider() cloudauth.Provider {
	return oauthProvider(DefaultEndpoints)
}

// oauthProvider is OAuthProvider over the OAuth server of e.
func oauthProvider(e Endpoints) cloudauth.Provider {
	oauth := strings.TrimSuffix(e.OAuth, "/")

	return cloudauth.Provider{
		Kind:           domain.KindYandexDisk,
		Name:           "Yandex Disk",
		Issuer:         "Yandex",
		AuthorizeURL:   oauth + "/authorize",
		TokenURL:       oauth + "/token",
		LoopbackAddr:   LoopbackAddr,
		CallbackPath:   "/callback",
		ManualRedirect: manualRedirect,
	}
}
