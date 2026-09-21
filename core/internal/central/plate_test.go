package central

import "testing"

func TestCanonicalPlate(t *testing.T) {
	cases := []struct {
		name         string
		raw          string
		want         string
		isWellFormed bool
	}{
		{name: "a spaced lowercase plate loses the space and takes upper case", raw: "abc 123", want: "ABC123", isWellFormed: true},
		{name: "a hyphenated plate loses the hyphen", raw: "ABC-123", want: "ABC123", isWellFormed: true},
		{name: "a taxi plate loses its trailing T", raw: "ABC12AT", want: "ABC12A", isWellFormed: true},
		{name: "a plate longer than seven characters is refused", raw: "ABCD12345", isWellFormed: false},
		{name: "an empty plate is refused", raw: "", isWellFormed: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plate, isWellFormed := canonicalPlate(tc.raw)

			if plate != tc.want || isWellFormed != tc.isWellFormed {
				t.Fatalf("canonicalPlate(%q) = %q, %v, want %q, %v", tc.raw, plate, isWellFormed, tc.want, tc.isWellFormed)
			}
		})
	}
}
