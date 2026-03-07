package handler

import (
	"fmt"
	"regexp"
)

// validPathParam matches values that are safe for use in tag filter queries.
// Allowed: alphanumeric, spaces, hyphens, underscores, dots, plus signs.
// Disallowed: single quotes, double quotes, semicolons, etc.
var validPathParam = regexp.MustCompile(`^[a-zA-Z0-9 \-_\.+]+$`)

// validatePathParam checks that a user-supplied path parameter is safe.
// Returns an error if the value contains characters that could break
// tag filter query syntax (e.g. single quotes for query injection).
func validatePathParam(name, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", name)
	}
	if len(value) > 256 {
		return fmt.Errorf("%s exceeds maximum length", name)
	}
	if !validPathParam.MatchString(value) {
		return fmt.Errorf("%s contains invalid characters", name)
	}
	return nil
}
