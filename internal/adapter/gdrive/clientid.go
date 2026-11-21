package gdrive

// BuiltinClientID and BuiltinClientSecret are the Desktop OAuth client of the
// Smartnote app registered in Google Cloud (spec §1, §5.6). They are not in
// the source repository: a release build sets them from the CI secrets with
//
//	-ldflags "-X github.com/nikchis/smartnote/internal/adapter/gdrive.BuiltinClientID=...
//	          -X github.com/nikchis/smartnote/internal/adapter/gdrive.BuiltinClientSecret=..."
//
// (see docs/building.md). A build without both has no built-in Google client:
// Google Drive then works only with an own client set in Advanced.
//
// Google requires the client secret of a Desktop client for the code
// exchange and for the refresh (П2), so it is built in by the customer's
// decision (spec §1). An installed app cannot keep a secret, so Google does
// not treat it as confidential: PKCE protects the exchange. It is still never
// logged and never reaches a DTO.
var (
	BuiltinClientID     string
	BuiltinClientSecret string
)
