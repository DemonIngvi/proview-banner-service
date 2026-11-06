// Package useragent classifies a User-Agent header into a coarse device
// class. It is deliberately simple and allocation-light: precise parsing is
// not needed for targeting and analytics slices.
package useragent

import "strings"

var botMarkers = []string{"bot", "crawler", "spider", "slurp", "headless", "lighthouse", "curl/", "wget/", "python-requests", "go-http-client"}

// Device returns one of "desktop", "mobile", "tablet" or "bot".
func Device(ua string) string {
	if ua == "" {
		return "bot"
	}
	l := strings.ToLower(ua)
	for _, m := range botMarkers {
		if strings.Contains(l, m) {
			return "bot"
		}
	}
	switch {
	case strings.Contains(l, "ipad"), strings.Contains(l, "tablet"),
		strings.Contains(l, "android") && !strings.Contains(l, "mobile"):
		return "tablet"
	case strings.Contains(l, "mobi"), strings.Contains(l, "iphone"), strings.Contains(l, "android"):
		return "mobile"
	default:
		return "desktop"
	}
}

// Country normalises a two-letter country code from a CDN/ingress header
// (for example CF-IPCountry or X-Country). Unknown values yield "".
func Country(v string) string {
	if len(v) != 2 {
		return ""
	}
	a, b := v[0]&^0x20, v[1]&^0x20 // upper-case ASCII letters
	if a < 'A' || a > 'Z' || b < 'A' || b > 'Z' {
		return ""
	}
	if a == 'X' && (b == 'X' || b == 'Y') { // "XX"/"XY" are "unknown" in common CDNs
		return ""
	}
	return string([]byte{a, b})
}
