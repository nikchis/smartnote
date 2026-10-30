package domain

// KeyPair is the client SSH key.
type KeyPair struct {
	PrivatePEM       []byte // OpenSSH private key, not passphrase-protected
	PublicAuthorized string // public key in authorized_keys format
}
