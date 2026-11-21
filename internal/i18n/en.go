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

// The titles of the native file dialogs.
const (
	DialogImportKey Key = "dialog.import_key"
	DialogExportKey Key = "dialog.export_key"
)

// The messages of the errors (UIError, spec §3.5). {provider} is the name of
// a cloud.
const (
	ErrWrongPassword        Key = "error.wrong_password"
	ErrCorruptStorage       Key = "error.corrupt_storage"
	ErrUnsupportedFormat    Key = "error.unsupported_format"
	ErrWrongSyncPhrase      Key = "error.wrong_sync_phrase"
	ErrNotConnected         Key = "error.not_connected"
	ErrRemoteChanged        Key = "error.remote_changed"
	ErrLocalChanged         Key = "error.local_changed"
	ErrKeyMissing           Key = "error.key_missing"
	ErrKeyPassphrase        Key = "error.key_passphrase_required" //nolint:gosec // a catalog key, not a credential
	ErrBusy                 Key = "error.busy"
	ErrReauthRequired       Key = "error.reauth_required"
	ErrRemoteDamaged        Key = "error.remote_damaged"
	ErrRemoteTooLarge       Key = "error.remote_too_large"
	ErrRemoteOutcomeUnknown Key = "error.remote_outcome_unknown"
	ErrLoopbackUnavailable  Key = "error.loopback_unavailable"
	ErrLoopbackNoCode       Key = "error.loopback_unavailable_no_code"
	ErrSearchUnavailable    Key = "error.search_unavailable"
	ErrTimeout              Key = "error.timeout"
	ErrCanceled             Key = "error.canceled"
	ErrHostKeyUnknown       Key = "error.host_key_unknown"  // {fingerprint}
	ErrHostKeyMismatch      Key = "error.host_key_mismatch" // {expected}, {got}
	ErrSignInFirst          Key = "error.sign_in_first"
	ErrCloudNoSpace         Key = "error.cloud.no_space"
	ErrCloudTooLarge        Key = "error.cloud.too_large"
	ErrCloudBusy            Key = "error.cloud.busy"
	ErrCloudStatus          Key = "error.cloud.status" // {status}, {code}
	ErrCloudCode            Key = "error.cloud.code"   // {code}
)

// The names of the fields in the validation messages; the key of the field f
// is "field." + f. The English names are the field identifiers, as the
// messages of 2.2.2 show them.
const (
	FieldName               Key = "field.name"
	FieldInfo               Key = "field.info"
	FieldPassword           Key = "field.password"
	FieldConfirm            Key = "field.confirm"
	FieldID                 Key = "field.id"
	FieldKind               Key = "field.kind"
	FieldYandexClientID     Key = "field.yandex_client_id"
	FieldGoogleClientID     Key = "field.google_client_id"
	FieldGoogleClientSecret Key = "field.google_client_secret"
	FieldSyncPhrase         Key = "field.sync_phrase"
	FieldSSHHost            Key = "field.ssh_host"
	FieldSSHPort            Key = "field.ssh_port"
	FieldSSHUser            Key = "field.ssh_user"
	FieldDBHost             Key = "field.db_host"
	FieldDBPort             Key = "field.db_port"
	FieldDBName             Key = "field.db_name"
	FieldDBUser             Key = "field.db_user"
	FieldTrustedHostKey     Key = "field.trusted_host_key"
	FieldKeyFile            Key = "field.key_file"
	FieldCode               Key = "field.code"
	FieldLanguage           Key = "field.language"
)

// The validation messages, one per reason. {field} is the name of the field;
// the English texts are those of domain.ValidationError.
const (
	InvalidRequired       Key = "invalid.required"
	InvalidRequiredShort  Key = "invalid.required_short"
	InvalidControl        Key = "invalid.control"
	InvalidMinChars       Key = "invalid.min_chars" // {n}
	InvalidMaxChars       Key = "invalid.max_chars" // {n}
	InvalidNameTaken      Key = "invalid.name_taken"
	InvalidConfirm        Key = "invalid.confirm"
	InvalidUUID           Key = "invalid.uuid"
	InvalidKind           Key = "invalid.kind"
	InvalidNotCloud       Key = "invalid.not_cloud"
	InvalidYandexClientID Key = "invalid.yandex_client_id"
	InvalidGoogleClientID Key = "invalid.google_client_id"
	InvalidClientSecret   Key = "invalid.client_secret"
	InvalidDBHost         Key = "invalid.db_host"
	InvalidPort           Key = "invalid.port"
	InvalidNotOffered     Key = "invalid.not_offered"
	InvalidOfferedOther   Key = "invalid.offered_elsewhere"
	InvalidOwnClient      Key = "invalid.own_client"
	InvalidKeyTooLarge    Key = "invalid.key_too_large"
	InvalidKeyDSA         Key = "invalid.key_dsa"
	InvalidKeyRSA         Key = "invalid.key_rsa" // {bits}, {min}
	InvalidLanguage       Key = "invalid.language"
)

var en = map[Key]string{ //nolint:gosec // catalog texts, not credentials
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

	DialogImportKey: "Import SSH private key",
	DialogExportKey: "Export SSH public key",

	ErrWrongPassword:        "Access to local storage denied.",
	ErrCorruptStorage:       "Local storage is damaged and cannot be opened.",
	ErrUnsupportedFormat:    "Data was created by a newer version of Smartnote.",
	ErrWrongSyncPhrase:      "The sync phrase does not match the remote storage.",
	ErrNotConnected:         "Remote storage is not configured.",
	ErrRemoteChanged:        "Remote data changed during the operation. Please try again.",
	ErrLocalChanged:         "Local data changed during the operation. Please try again.",
	ErrKeyMissing:           "There is no SSH key. Generate or import a key first.",
	ErrKeyPassphrase:        "The key is protected with a passphrase.",
	ErrBusy:                 "Another operation with the remote storage is in progress.",
	ErrReauthRequired:       "Sign in to {provider} again.",
	ErrRemoteDamaged:        "Data in the remote storage is damaged.",
	ErrRemoteTooLarge:       "Notes do not fit into the remote storage (64 MiB).",
	ErrRemoteOutcomeUnknown: "The result of the backup is unknown. Connect again to check the remote storage.",
	ErrLoopbackUnavailable:  "The local sign-in address is unavailable. Enter the code manually.",
	ErrLoopbackNoCode:       "Could not start sign-in on this computer. Try again.",
	ErrSearchUnavailable:    "Search unavailable",
	ErrTimeout:              "The operation timed out.",
	ErrCanceled:             "The operation was canceled.",
	ErrHostKeyUnknown:       "unknown server host key {fingerprint}",
	ErrHostKeyMismatch:      "server host key mismatch: expected {expected}, got {got}",
	ErrSignInFirst:          "Sign in to {provider} first, or choose None (local only).",
	ErrCloudNoSpace:         "Not enough space on {provider}.",
	ErrCloudTooLarge:        "The file is larger than your {provider} plan allows.",
	ErrCloudBusy:            "{provider} is busy. Try again later.",
	ErrCloudStatus:          "{provider} error: {status} {code}.",
	ErrCloudCode:            "{provider} error: {code}.",

	FieldName:               "name",
	FieldInfo:               "info",
	FieldPassword:           "password",
	FieldConfirm:            "confirm",
	FieldID:                 "id",
	FieldKind:               "kind",
	FieldYandexClientID:     "yandex_client_id",
	FieldGoogleClientID:     "google_client_id",
	FieldGoogleClientSecret: "google_client_secret",
	FieldSyncPhrase:         "sync_phrase",
	FieldSSHHost:            "ssh_host",
	FieldSSHPort:            "ssh_port",
	FieldSSHUser:            "ssh_user",
	FieldDBHost:             "db_host",
	FieldDBPort:             "db_port",
	FieldDBName:             "db_name",
	FieldDBUser:             "db_user",
	FieldTrustedHostKey:     "trusted_host_key",
	FieldKeyFile:            "key_file",
	FieldCode:               "code",
	FieldLanguage:           "language",

	InvalidRequired:       "invalid {field}: is required",
	InvalidRequiredShort:  "invalid {field}: required",
	InvalidControl:        "invalid {field}: must not contain control characters",
	InvalidMinChars:       "invalid {field}: must be at least {n} characters",
	InvalidMaxChars:       "invalid {field}: must be at most {n} characters",
	InvalidNameTaken:      "invalid {field}: a note with this name already exists",
	InvalidConfirm:        "invalid {field}: does not match the password",
	InvalidUUID:           "invalid {field}: is not a UUID",
	InvalidKind:           "invalid {field}: unknown storage kind",
	InvalidNotCloud:       "invalid {field}: is not a cloud storage",
	InvalidYandexClientID: "invalid {field}: must be 1 to 64 letters, digits, '_' or '-'",
	InvalidGoogleClientID: "invalid {field}: must be a Google OAuth client ID",
	InvalidClientSecret:   "invalid {field}: must be 1 to 128 printable ASCII characters",
	InvalidDBHost:         "invalid {field}: must be localhost, 127.0.0.1 or ::1",
	InvalidPort:           "invalid {field}: must be between 1 and 65535",
	InvalidNotOffered:     "invalid {field}: was not offered by the server",
	InvalidOfferedOther:   "invalid {field}: was offered by another server",
	InvalidOwnClient:      "invalid {field}: set your own client ID in Advanced",
	InvalidKeyTooLarge:    "invalid {field}: is too large for a private key",
	InvalidKeyDSA:         "invalid {field}: is a DSA key, which is no longer secure; use Ed25519 or RSA of at least 2048 bits",
	InvalidKeyRSA:         "invalid {field}: is an RSA key of {bits} bits; at least {min} are needed",
	InvalidLanguage:       "invalid {field}: must be en or ru",
}
