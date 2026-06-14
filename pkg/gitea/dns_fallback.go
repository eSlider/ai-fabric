package gitea

import (
	"net"
	"net/url"
	"strings"
)

// FallbackBaseURLForDNSError rewrites a gitea hostname base URL to localhost when
// err is a DNS lookup/dial failure against that host (bot-on-host workaround).
// ponytail: hostname=="gitea" only; upgrade: configurable host→localhost map.
func FallbackBaseURLForDNSError(baseURL string, err error) (string, bool) {
	if err == nil || baseURL == "" {
		return "", false
	}
	base, parseErr := url.Parse(baseURL)
	if parseErr != nil {
		return "", false
	}

	hostname := strings.ToLower(strings.TrimSpace(base.Hostname()))
	if hostname != "gitea" {
		return "", false
	}

	lowerErr := strings.ToLower(err.Error())
	if !strings.Contains(lowerErr, "lookup "+hostname) || !strings.Contains(lowerErr, "dial tcp") {
		return "", false
	}

	if port := base.Port(); port != "" {
		base.Host = net.JoinHostPort("localhost", port)
	} else {
		base.Host = "localhost"
	}
	return strings.TrimRight(base.String(), "/"), true
}
