package crontool

import "regexp"

// maxCronNameLen is the maximum allowed length for a cron name.
const maxCronNameLen = 128

// validCronName matches alphanumeric characters, hyphens, and underscores.
var validCronName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

// isValidCronName returns true if name is a valid cron identifier:
// alphanumeric start, followed by alphanumeric, hyphens, or underscores,
// with a maximum length of 128 characters.
func isValidCronName(name string) bool {
	if len(name) == 0 || len(name) > maxCronNameLen {
		return false
	}
	return validCronName.MatchString(name)
}
