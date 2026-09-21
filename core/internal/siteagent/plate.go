// Package siteagent decides entry at one wash site from its own local copy of who is allowed in.
package siteagent

import (
	"regexp"
	"strings"
)

var (
	acceptedPlate = regexp.MustCompile(`^[A-Z0-9]{2,7}$`)
	taxiPlate     = regexp.MustCompile(`^([A-Z]{3}[0-9]{2}[A-Z0-9])T$`)
	separators    = strings.NewReplacer(" ", "", "-", "")
)

// NormalizePlate turns a raw plate read into the key vehicles are registered under,
// reporting false when the read matches no accepted plate shape.
func NormalizePlate(read string) (string, bool) {
	plate := strings.ToUpper(separators.Replace(read))
	if !acceptedPlate.MatchString(plate) {
		return "", false
	}
	// A taxi's trailing T is a marking on the plate, not part of its registration.
	if match := taxiPlate.FindStringSubmatch(plate); match != nil {
		return match[1], true
	}
	return plate, true
}
