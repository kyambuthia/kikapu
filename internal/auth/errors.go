package auth

import (
	"net/http"

	"github.com/kyambuthia/kikapu/internal/platform/httpx"
)

func WriteAuthError(w http.ResponseWriter, r *http.Request, err error, prefix string) {
	var code, message string
	status := http.StatusUnauthorized

	switch err {
	case ErrMissingKey:
		code = "missing_key"
		message = "API key is required in Authorization header"
	case ErrKeyInvalid:
		code = "invalid_key"
		message = "API key is invalid"
	case ErrKeyNotFound:
		code = "invalid_key"
		message = "API key not found"
	case ErrKeyExpired:
		code = "expired_key"
		message = "API key has expired"
	case ErrKeyRevoked:
		code = "revoked_key"
		message = "API key has been revoked"
	default:
		code = "invalid_key"
		message = "failed to authenticate request"
	}

	requestID := httpx.RequestID(r)
	httpx.WriteError(w, status, code, message, requestID)
}

func WriteScopeError(w http.ResponseWriter, r *http.Request, requiredScope, prefix string) {
	status := http.StatusForbidden
	message := "insufficient permissions"
	errorCode := "missing_scope"

	requestID := httpx.RequestID(r)
	httpx.WriteJSON(w, status, map[string]interface{}{
		"error": map[string]interface{}{
			"code":           errorCode,
			"message":        message,
			"required_scope": requiredScope,
			"request_id":     requestID,
		},
	})
}
