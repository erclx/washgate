package stripe

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"
)

// signatureTolerance is how far a signed timestamp may sit from now before a replay is assumed.
const signatureTolerance = 300 * time.Second

var (
	// ErrMissingSignature reports a webhook request with no Stripe-Signature header.
	ErrMissingSignature = errors.New("missing Stripe-Signature header")
	// ErrMalformedSignature reports a Stripe-Signature header without a timestamp or a v1 signature.
	ErrMalformedSignature = errors.New("malformed Stripe-Signature header")
	// ErrStaleSignature reports a signature whose timestamp is outside the tolerance around now.
	ErrStaleSignature = errors.New("stale Stripe-Signature timestamp")
	// ErrSignatureMismatch reports a payload no v1 signature in the header was made over with the secret.
	ErrSignatureMismatch = errors.New("no Stripe-Signature matches the payload")
)

// VerifySignature checks that header holds a v1 signature over payload made with secret, timestamped near now.
func VerifySignature(payload []byte, header, secret string, now time.Time) error {
	if header == "" {
		return ErrMissingSignature
	}
	var timestamp string
	var signatures [][]byte
	for pair := range strings.SplitSeq(header, ",") {
		key, value, _ := strings.Cut(strings.TrimSpace(pair), "=")
		switch key {
		case "t":
			timestamp = value
		case "v1":
			decoded, err := hex.DecodeString(value)
			if err == nil {
				signatures = append(signatures, decoded)
			}
		}
	}
	signedAt, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || len(signatures) == 0 {
		return ErrMalformedSignature
	}
	if age := now.Sub(time.Unix(signedAt, 0)); age > signatureTolerance || age < -signatureTolerance {
		return ErrStaleSignature
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(payload)
	expected := mac.Sum(nil)
	for _, signature := range signatures {
		if hmac.Equal(signature, expected) {
			return nil
		}
	}
	return ErrSignatureMismatch
}
