package services

import (
	"net"
	"net/url"
	"strings"
)

func normalizeURL(value string) string {
	trimmed := strings.TrimSpace(value)
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return trimmed
	}

	parsed.Scheme = strings.ToLower(parsed.Scheme)
	hostname := strings.ToLower(parsed.Hostname())
	if port := parsed.Port(); port != "" {
		parsed.Host = net.JoinHostPort(hostname, port)
	} else if strings.Contains(hostname, ":") {
		parsed.Host = "[" + hostname + "]"
	} else {
		parsed.Host = hostname
	}
	// An empty HTTP path and "/" produce the same request target. Other
	// trailing slashes remain untouched because path identity can be sensitive.
	if parsed.Path == "/" && parsed.RawPath == "" && parsed.RawQuery == "" {
		parsed.Path = ""
	}
	return parsed.String()
}
