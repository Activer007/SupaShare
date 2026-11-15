// Package serviceurl validates explicit CLI service destinations before secrets
// can be sent. It deliberately supports only unambiguous ASCII host/path forms.
package serviceurl

import (
	"errors"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

var ErrInvalid = errors.New("invalid service URL: use http(s) with a host and optional simple base path, without credentials, query or fragment")

// Normalize preserves a reverse-proxy base path (case-sensitive), with no
// encoded segments, dot traversal or duplicate slashes. It never performs DNS
// or follows redirects. Unicode hosts must be supplied as explicit ASCII IDNA.
func Normalize(raw string) (string, error) {
	if raw == "" || len(raw) > 2048 || strings.ContainsAny(raw, "?#\\") {
		return "", ErrInvalid
	}
	for _, c := range raw {
		if c < 33 || c > 126 {
			return "", ErrInvalid
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" || u.User != nil || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
		return "", ErrInvalid
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "https" && scheme != "http" {
		return "", ErrInvalid
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || strings.Contains(host, "%") {
		return "", ErrInvalid
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		if ip.Zone() != "" {
			return "", ErrInvalid
		}
		host = ip.String()
	} else {
		if strings.ContainsAny(u.Host, "[]") {
			return "", ErrInvalid
		}
		if strings.ContainsAny(host, ":[]") || len(host) > 253 {
			return "", ErrInvalid
		}
		for _, part := range strings.Split(host, ".") {
			if len(part) == 0 || len(part) > 63 || part[0] == '-' || part[len(part)-1] == '-' {
				return "", ErrInvalid
			}
			for _, c := range part {
				if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
					return "", ErrInvalid
				}
			}
		}
	}
	port := u.Port()
	authority := host
	if strings.Contains(host, ":") {
		authority = "[" + host + "]"
	}
	if port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return "", ErrInvalid
		}
		port = strconv.Itoa(number)
		if !(scheme == "http" && port == "80" || scheme == "https" && port == "443") {
			authority = net.JoinHostPort(host, port)
		}
	} else if strings.HasSuffix(u.Host, ":") {
		return "", ErrInvalid
	}
	if u.RawPath != "" || strings.Contains(u.EscapedPath(), "%") || strings.Contains(u.Path, "//") {
		return "", ErrInvalid
	}
	for _, segment := range strings.Split(u.Path, "/") {
		if segment == "." || segment == ".." {
			return "", ErrInvalid
		}
		for _, c := range segment {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-._~", c)) {
				return "", ErrInvalid
			}
		}
	}
	return scheme + "://" + authority + strings.TrimSuffix(u.Path, "/"), nil
}
