package webhook

import "errors"

var (
	ErrMissingSignature   = errors.New("missing signature")
	ErrInvalidSignature   = errors.New("invalid signature")
	ErrStaleTimestamp     = errors.New("stale timestamp")
	ErrMalformedSignature = errors.New("malformed signature header")
)
