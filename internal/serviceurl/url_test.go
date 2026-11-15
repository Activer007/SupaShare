package serviceurl

import "testing"

func TestNormalize(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"https://EXAMPLE.com:443/", "https://example.com"},
		{"http://localhost:80/proxy/", "http://localhost/proxy"},
		{"https://example.com/Team/App", "https://example.com/Team/App"},
		{"http://[::1]:8080/base", "http://[::1]:8080/base"},
		{"https://[2001:db8::1]:443/", "https://[2001:db8::1]"},
		{"http://127.0.0.1:1234", "http://127.0.0.1:1234"},
	} {
		got, err := Normalize(tc.raw)
		if err != nil || got != tc.want {
			t.Fatalf("normalize %q = %q, %v", tc.raw, got, err)
		}
	}
	for _, raw := range []string{
		"", "//example.com", "https://", "ftp://example.com", "https://trusted.example@evil.example", "https://user:pass@evil.example", "https://host?", "https://host#", "https://host?q=x", "https://host/#part", "https://host\\evil", "https://host\n", "https://host:0", "https://host:65536", "https://host:", "https://host:bad", "https://host/a/../b", "https://host/./", "https://host//a", "https://host/%2fadmin", "https://host/%61dmin", "https://host/path with spaces", "https://[fe80::1%25eth0]", "https://例子.test", "https://host./", "https://-host", "https://[not-ip]",
	} {
		if got, err := Normalize(raw); err == nil || got != "" {
			t.Fatalf("unsafe URL accepted: %q => %q", raw, got)
		}
	}
}
