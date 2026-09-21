package central

import (
	"regexp"
	"strings"
)

// taxiPlate matches a taxi's registration followed by the T marking, the same shape the site drops it from.
var taxiPlate = regexp.MustCompile(`^([A-Z]{3}[0-9]{2}[A-Z0-9])T$`)

var plateSeparators = strings.NewReplacer(" ", "", "-", "")

// canonicalPlate turns a plate a person typed into the key central stores it under, the way the lane
// normalizes a read: separators dropped, upper case, and a taxi's trailing T removed. Every route that takes
// a typed plate reads it through here, so a plate registered, bought, and looked up lands on one key.
func canonicalPlate(raw string) (string, bool) {
	plate := strings.ToUpper(plateSeparators.Replace(strings.TrimSpace(raw)))
	if !normalizedPlate.MatchString(plate) {
		return "", false
	}
	if match := taxiPlate.FindStringSubmatch(plate); match != nil {
		return match[1], true
	}
	return plate, true
}
