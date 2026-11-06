package api

import (
	"regexp"
	"strings"
)

var (
	idPattern      = regexp.MustCompile(`^[A-Za-z0-9._:@-]{1,128}$`)
	countryPattern = regexp.MustCompile(`^[A-Za-z]{2}$`)
)

func validID(field, value string) error {
	if value == "" {
		return invalid("%s is required", field)
	}
	if !idPattern.MatchString(value) {
		return invalid("%s must be 1-128 characters from A-Z a-z 0-9 . _ : @ -", field)
	}
	return nil
}

// validCountry checks the format and returns the upper-case code.
func validCountry(value string) (string, error) {
	if value == "" {
		return "", invalid("country is required")
	}
	if !countryPattern.MatchString(value) {
		return "", invalid("country must be a two-letter ISO code")
	}
	return strings.ToUpper(value), nil
}

func validStore(value string) error {
	switch value {
	case "app_store", "play":
		return nil
	case "":
		return invalid("store is required")
	}
	return invalid("store must be app_store or play")
}
