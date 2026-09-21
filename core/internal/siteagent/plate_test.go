package siteagent

import "testing"

func TestNormalizePlateAcceptsKnownShapes(t *testing.T) {
	cases := []struct {
		name string
		read string
		want string
	}{
		{name: "standard with three digits", read: "ABC123", want: "ABC123"},
		{name: "standard ending in a letter", read: "ABC12A", want: "ABC12A"},
		{name: "lowercase with a space", read: "abc 123", want: "ABC123"},
		{name: "hyphenated", read: "ABC-123", want: "ABC123"},
		{name: "taxi drops its trailing T", read: "ABC123T", want: "ABC123"},
		{name: "taxi on a letter-ending plate", read: "ABC12AT", want: "ABC12A"},
		{name: "personalized at two characters", read: "AB", want: "AB"},
		{name: "personalized at seven characters", read: "SPARKLE", want: "SPARKLE"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := NormalizePlate(tc.read)

			if !ok {
				t.Fatalf("NormalizePlate(%q) rejected the read, want %q", tc.read, tc.want)
			}
			if got != tc.want {
				t.Errorf("NormalizePlate(%q) = %q, want %q", tc.read, got, tc.want)
			}
		})
	}
}

func TestNormalizePlateRejectsMalformedReads(t *testing.T) {
	cases := []struct {
		name string
		read string
	}{
		{name: "one character", read: "A"},
		{name: "eight characters", read: "ABCD1234"},
		{name: "punctuation", read: "AB!123"},
		{name: "letter outside A to Z", read: "ÅSA123"},
		{name: "empty", read: ""},
		{name: "only separators", read: " - "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := NormalizePlate(tc.read)

			if ok {
				t.Errorf("NormalizePlate(%q) = %q, want a rejection", tc.read, got)
			}
		})
	}
}
