package i18n

// The texts of the pages the browser shows after the OAuth sign-in. {provider}
// is the name of the cloud; {issuer} is the name of the service that signs the
// user in.
const (
	AuthSignedInTitle  Key = "auth.signed_in.title"
	AuthSignedInText   Key = "auth.signed_in.text"
	AuthCancelledTitle Key = "auth.cancelled.title"
	AuthCancelledText  Key = "auth.cancelled.text"
	AuthFailedTitle    Key = "auth.failed.title"
	AuthFailedText     Key = "auth.failed.text"
	AuthExpiredTitle   Key = "auth.expired.title"
	AuthExpiredText    Key = "auth.expired.text"
	AuthNotFoundTitle  Key = "auth.not_found.title"
	AuthNotFoundText   Key = "auth.not_found.text"
	AuthCloseHint      Key = "auth.close_hint"
)

var en = map[Key]string{
	AuthSignedInTitle:  "Signed in to {provider}",
	AuthSignedInText:   "Return to Smartnote and press OK in Settings to save the sign-in.",
	AuthCancelledTitle: "Sign-in cancelled",
	AuthCancelledText:  "Smartnote did not get access to {provider}. You can sign in again from Settings.",
	AuthFailedTitle:    "Sign-in failed",
	AuthFailedText:     "{issuer} did not complete the sign-in. Return to Smartnote and try again.",
	AuthExpiredTitle:   "Sign-in link expired",
	AuthExpiredText:    "This sign-in link has expired or was already used. Start the sign-in again from Smartnote Settings.",
	AuthNotFoundTitle:  "Page not found",
	AuthNotFoundText:   "This address is not part of Smartnote.",
	AuthCloseHint:      "You can close this tab.",
}
