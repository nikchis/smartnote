package usecase

import (
	"errors"
	"sync"

	"github.com/nikchis/smartnote/internal/domain"
)

// HostKeyOffer keeps the last server key presented as unknown, with the
// address that presented it. The user confirms it by fingerprint, so the key
// itself never comes back from the interface, and it is trusted only for that
// address. Sync and Connection share one offer. The zero value is ready to use.
type HostKeyOffer struct {
	mu   sync.Mutex
	last *domain.HostKeyUnknownError
}

// remember keeps the offer carried by err, if any.
func (o *HostKeyOffer) remember(err error) {
	hk, ok := errors.AsType[*domain.HostKeyUnknownError](err)
	if !ok {
		return
	}

	offer := *hk

	o.mu.Lock()
	defer o.mu.Unlock()

	o.last = &offer
}

// take returns and forgets the offer with fingerprint presented by addr. An
// offer that does not match stays for a later call.
func (o *HostKeyOffer) take(fingerprint, addr string) (domain.HostKeyUnknownError, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	switch {
	case o.last == nil || fingerprint == "" || o.last.Fingerprint != fingerprint:
		return domain.HostKeyUnknownError{}, errNotOffered
	case o.last.Addr != addr:
		return domain.HostKeyUnknownError{}, errOfferedElsewhere
	}

	offer := *o.last
	o.last = nil

	return offer, nil
}

// fieldTrustedHostKey names the trusted key in validation errors.
const fieldTrustedHostKey = "trusted_host_key"

// Errors of a trust request that no matching offer backs.
var (
	errNotOffered       = &domain.ValidationError{Field: fieldTrustedHostKey, Reason: "was not offered by the server"}
	errOfferedElsewhere = &domain.ValidationError{Field: fieldTrustedHostKey, Reason: "was offered by another server"}
)
