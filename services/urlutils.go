package services

import "strings"

func normalizeURL(value string) string {
	trimmed := strings.TrimSpace(value)
	trimmed = strings.TrimRight(trimmed, "/")
	return strings.ToLower(trimmed)
}
