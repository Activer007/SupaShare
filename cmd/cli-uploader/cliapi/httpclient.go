package cliapi

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Only file downloads may leave the API origin, to fetch a presigned storage
// URL. Control-plane redirects must not replay upload bodies or metadata to a
// different server. Preserve the caller's existing timeout (zero for streams).
func newHTTPClient(timeout time.Duration, storageDownload bool) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("stopped after 10 redirects")
			}
			if req.URL.User != nil || (req.URL.Scheme != "http" && req.URL.Scheme != "https") {
				return errors.New("redirect requires an HTTP(S) URL without user credentials")
			}
			for _, previous := range via {
				if previous.URL.Scheme == "https" && req.URL.Scheme == "http" {
					return errors.New("HTTPS to HTTP redirect refused")
				}
			}
			// Go recopies the initial headers at every hop. Once the chain has
			// left the original origin, never restore them, even on a return hop.
			crossedOrigin := !sameOrigin(via[0].URL, req.URL)
			for _, previous := range via[1:] {
				crossedOrigin = crossedOrigin || !sameOrigin(via[0].URL, previous.URL)
			}
			if crossedOrigin {
				if !storageDownload || req.Method != http.MethodGet || req.Body != nil {
					return errors.New("cross-origin API redirect refused; configure the canonical server URL")
				}
				// Do not forward API keys, passwords, cookies, Authorization,
				// Referer or future application headers to object storage.
				req.Header = make(http.Header)
				req.Host = ""
			}
			return nil
		},
	}
}

func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) &&
		strings.EqualFold(a.Hostname(), b.Hostname()) && effectivePort(a) == effectivePort(b)
}

func effectivePort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	if strings.EqualFold(u.Scheme, "https") {
		return "443"
	}
	return "80"
}
