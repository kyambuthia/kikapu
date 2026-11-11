package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func Sign(secret string, ts time.Time, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(fmt.Sprintf("%d", ts.Unix())))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func BuildSignatureHeader(ts time.Time, signature string) string {
	return fmt.Sprintf("t=%d,v1=%s", ts.Unix(), signature)
}

func Verify(secret string, header string, body []byte, maxAge time.Duration) error {
	if header == "" {
		return ErrMissingSignature
	}

	ts, sig, err := ParseSignatureHeader(header)
	if err != nil {
		return err
	}

	if time.Since(ts) > maxAge {
		return ErrStaleTimestamp
	}

	expectedSig := Sign(secret, ts, body)

	if subtle.ConstantTimeCompare([]byte(sig), []byte(expectedSig)) != 1 {
		return ErrInvalidSignature
	}

	return nil
}

func ParseSignatureHeader(header string) (time.Time, string, error) {
	parts := strings.Split(header, ",")
	if len(parts) < 2 {
		return time.Time{}, "", ErrMalformedSignature
	}

	var ts time.Time
	var sig string

	for _, part := range parts {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			return time.Time{}, "", ErrMalformedSignature
		}

		switch kv[0] {
		case "t":
			unixTime, err := strconv.ParseInt(kv[1], 10, 64)
			if err != nil {
				return time.Time{}, "", ErrMalformedSignature
			}
			ts = time.Unix(unixTime, 0)
		case "v1":
			sig = kv[1]
		}
	}

	if ts.IsZero() || sig == "" {
		return time.Time{}, "", ErrMalformedSignature
	}

	return ts, sig, nil
}
