package gdrive

// BuiltinClientID and BuiltinClientSecret are the Desktop OAuth client of the
// Smartnote app registered in Google Cloud (spec §1, §5.6). Google requires
// the client secret of a Desktop client for the code exchange and for the
// refresh (П2), so it is built in by the customer's decision (spec §1). An
// installed app cannot keep a secret, so Google does not treat it as
// confidential: PKCE protects the exchange. It is still never logged.
const (
	BuiltinClientID     = "000000000000-builtin.apps.googleusercontent.com" //nolint:gosec // a client ID is public
	BuiltinClientSecret = "SET-AT-BUILD-TIME"                                     //nolint:gosec // a Desktop client secret is not confidential (spec §1)
)
