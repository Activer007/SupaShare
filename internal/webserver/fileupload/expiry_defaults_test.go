package fileupload

import (
	"net/url"
	"testing"
	"time"
)

func TestParseConfigExpiryDefaults(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		value     string
		days      int
		unlimited bool
	}{
		{name: "omitted", days: 5},
		{name: "explicit-unlimited", value: "0", unlimited: true},
		{name: "explicit-duration", value: "14", days: 14},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			values := url.Values{}
			if scenario.value != "" {
				values.Set("expiryDays", scenario.value)
			}
			before := time.Now().Add(time.Duration(scenario.days) * 24 * time.Hour).Unix()
			config, err := parseConfig(values)
			if err != nil {
				t.Fatal(err)
			}
			after := time.Now().Add(time.Duration(scenario.days) * 24 * time.Hour).Unix()
			if config.Expiry != scenario.days || config.UnlimitedTime != scenario.unlimited {
				t.Fatalf("expiry days=%d unlimited=%t, want days=%d unlimited=%t", config.Expiry, config.UnlimitedTime, scenario.days, scenario.unlimited)
			}
			if config.ExpiryTimestamp < before || config.ExpiryTimestamp > after {
				t.Fatalf("expiry timestamp=%d outside [%d, %d]", config.ExpiryTimestamp, before, after)
			}
		})
	}
}
