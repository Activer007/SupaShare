//go:build test

package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestChunkCompleteExpiryDefaults(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		header    string
		days      int
		unlimited bool
	}{
		{name: "omitted", days: 5},
		{name: "explicit-unlimited", header: "0", unlimited: true},
		{name: "explicit-duration", header: "14", days: 14},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/chunk/complete", nil)
			r.Header.Set("uuid", "expiry-default-test")
			r.Header.Set("filename", "expiry.txt")
			r.Header.Set("filesize", "1")
			if scenario.header != "" {
				r.Header.Set("expiryDays", scenario.header)
			}
			p := &paramChunkComplete{}
			if err := p.ParseRequest(r); err != nil {
				t.Fatal(err)
			}
			if p.ExpiryDays != scenario.days || p.UnlimitedTime != scenario.unlimited {
				t.Fatalf("expiry days=%d unlimited=%t, want days=%d unlimited=%t", p.ExpiryDays, p.UnlimitedTime, scenario.days, scenario.unlimited)
			}
		})
	}
}
