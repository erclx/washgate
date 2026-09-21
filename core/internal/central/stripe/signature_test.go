package stripe

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"
	"time"
)

const testWebhookSecret = "whsec_test_secret" //nolint:gosec // a fixed secret the tests sign with, never a real one

var (
	testPayload  = []byte(`{"id":"evt_1","type":"invoice.paid"}`)
	testSignedAt = time.Date(2026, 9, 21, 7, 30, 0, 0, time.UTC)
)

func signature(payload []byte, secret string, signedAt time.Time) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = fmt.Fprintf(mac, "%d.%s", signedAt.Unix(), payload)
	return hex.EncodeToString(mac.Sum(nil))
}

func signatureHeader(payload []byte, secret string, signedAt time.Time) string {
	return fmt.Sprintf("t=%d,v1=%s", signedAt.Unix(), signature(payload, secret, signedAt))
}

func TestVerifySignature(t *testing.T) {
	cases := []struct {
		name    string
		payload []byte
		header  string
		now     time.Time
		want    error
	}{
		{
			name:    "a signature over the payload with the secret passes",
			payload: testPayload,
			header:  signatureHeader(testPayload, testWebhookSecret, testSignedAt),
			now:     testSignedAt.Add(time.Minute),
		},
		{
			name:    "a second v1 entry that matches passes",
			payload: testPayload,
			header: fmt.Sprintf("t=%d,v1=%s,v1=%s", testSignedAt.Unix(),
				signature(testPayload, "whsec_rotated_out", testSignedAt), signature(testPayload, testWebhookSecret, testSignedAt)),
			now: testSignedAt,
		},
		{
			name:    "a signature made with another secret fails",
			payload: testPayload,
			header:  signatureHeader(testPayload, "whsec_other", testSignedAt),
			now:     testSignedAt,
			want:    ErrSignatureMismatch,
		},
		{
			name:    "a changed body fails",
			payload: []byte(`{"id":"evt_2","type":"invoice.paid"}`),
			header:  signatureHeader(testPayload, testWebhookSecret, testSignedAt),
			now:     testSignedAt,
			want:    ErrSignatureMismatch,
		},
		{
			name:    "a missing header fails",
			payload: testPayload,
			header:  "",
			now:     testSignedAt,
			want:    ErrMissingSignature,
		},
		{
			name:    "a header with no timestamp fails as malformed",
			payload: testPayload,
			header:  "v1=" + signature(testPayload, testWebhookSecret, testSignedAt),
			now:     testSignedAt,
			want:    ErrMalformedSignature,
		},
		{
			name:    "a header with no v1 entry fails as malformed",
			payload: testPayload,
			header:  fmt.Sprintf("t=%d,v0=abc", testSignedAt.Unix()),
			now:     testSignedAt,
			want:    ErrMalformedSignature,
		},
		{
			name:    "a timestamp more than five minutes old fails as stale",
			payload: testPayload,
			header:  signatureHeader(testPayload, testWebhookSecret, testSignedAt),
			now:     testSignedAt.Add(301 * time.Second),
			want:    ErrStaleSignature,
		},
		{
			name:    "a timestamp more than five minutes ahead fails as stale",
			payload: testPayload,
			header:  signatureHeader(testPayload, testWebhookSecret, testSignedAt),
			now:     testSignedAt.Add(-301 * time.Second),
			want:    ErrStaleSignature,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := VerifySignature(tc.payload, tc.header, testWebhookSecret, tc.now)

			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}
