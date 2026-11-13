package cliapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/cmd/cli-uploader/cliflags"
	"github.com/Activer007/SupaShare/internal/models"
)

func TestRedirectOriginPolicy(t *testing.T) {
	for _, tc := range []struct {
		from, to string
		same     bool
	}{
		{"https://API.example/path", "https://api.example:443/next", true},
		{"http://api.example", "http://api.example:80", true},
		{"https://api.example", "https://api.example:444", false},
		{"https://api.example", "https://sub.api.example", false},
		{"https://api.example", "https://api.example.evil", false},
		{"http://api.example:443", "https://api.example:443", false},
		{"https://[::1]", "https://[::1]:443", true},
	} {
		a, _ := url.Parse(tc.from)
		b, _ := url.Parse(tc.to)
		if sameOrigin(a, b) != tc.same {
			t.Errorf("origin comparison: %s -> %s", tc.from, tc.to)
		}
	}
	for _, tc := range []struct {
		name, from, to    string
		download, wantErr bool
	}{
		{"same", "https://a.test", "https://a.test/path", false, false},
		{"different host API", "https://a.test", "https://b.test", false, true},
		{"different host download", "https://a.test", "https://b.test", true, false},
		{"downgrade", "https://a.test", "http://a.test", true, true},
		{"upgrade", "http://a.test", "https://a.test", true, false},
		{"userinfo", "https://a.test", "https://user:pass@b.test", true, true},
		{"scheme", "https://a.test", "ftp://b.test", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first, _ := http.NewRequest("GET", tc.from, nil)
			next, _ := http.NewRequest("GET", tc.to, nil)
			next.Header.Set("apikey", "synthetic")
			next.Header.Set("password", "synthetic")
			next.Host = "a.test"
			err := newHTTPClient(time.Second, tc.download).CheckRedirect(next, []*http.Request{first})
			if (err != nil) != tc.wantErr {
				t.Fatalf("unexpected result: %v", err)
			}
			if !tc.wantErr && !sameOrigin(first.URL, next.URL) && (len(next.Header) != 0 || next.Host != "") {
				t.Fatal("cross-origin headers retained")
			}
		})
	}
}

func TestDownloadRedirectChains(t *testing.T) {
	for _, code := range []int{301, 302, 303, 307, 308} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			var apiURL string
			var seen int
			storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				seen++
				for _, h := range []string{"apikey", "password", "Authorization", "Cookie", "Referer", "X-Future-Secret"} {
					if r.Header.Get(h) != "" {
						t.Errorf("storage received %s", h)
					}
				}
				if r.URL.Path == "/first" {
					http.Redirect(w, r, "/second", code)
				} else {
					http.Redirect(w, r, apiURL+"/return", code)
				}
			}))
			defer storage.Close()
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/start":
					http.Redirect(w, r, "/same", code)
				case "/same":
					if r.Header.Get("apikey") != "synthetic" {
						t.Error("same-origin authentication lost")
					}
					http.Redirect(w, r, storage.URL+"/first?signature=synthetic", code)
				case "/return":
					for _, h := range []string{"apikey", "password", "Authorization", "Cookie", "Referer", "X-Future-Secret"} {
						if r.Header.Get(h) != "" {
							t.Errorf("return hop restored %s", h)
						}
					}
					io.WriteString(w, "file")
				}
			}))
			defer api.Close()
			apiURL = api.URL
			req, _ := http.NewRequest("GET", api.URL+"/start", nil)
			for _, h := range []string{"apikey", "password", "Authorization", "Cookie", "X-Future-Secret"} {
				req.Header.Set(h, "synthetic")
			}
			res, err := newHTTPClient(0, true).Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			body, _ := io.ReadAll(res.Body)
			if string(body) != "file" || seen != 2 {
				t.Fatal("download chain failed")
			}
		})
	}
}

func TestControlPlaneRedirectNeverSendsBody(t *testing.T) {
	for _, code := range []int{301, 302, 303, 307, 308} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("cross-origin API target contacted") }))
			defer target.Close()
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, code) }))
			defer source.Close()
			req, _ := http.NewRequest("POST", source.URL, bytes.NewBufferString("synthetic-private-body"))
			req.Header.Set("apikey", "synthetic")
			res, err := newHTTPClient(0, false).Do(req)
			if res != nil {
				res.Body.Close()
			}
			if err == nil {
				t.Fatal("cross-origin API redirect accepted")
			}
		})
	}
}

func TestRedirectDowngradeAndLimit(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("downgrade contacted") }))
	defer target.Close()
	tls := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer tls.Close()
	client := newHTTPClient(0, true)
	client.Transport = tls.Client().Transport
	res, err := client.Get(tls.URL)
	if res != nil {
		res.Body.Close()
	}
	if err == nil {
		t.Fatal("downgrade accepted")
	}
	count := 0
	loop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { count++; http.Redirect(w, r, "/loop", 302) }))
	defer loop.Close()
	res, err = newHTTPClient(0, true).Get(loop.URL)
	if res != nil {
		res.Body.Close()
	}
	if err == nil || count != 10 {
		t.Fatalf("redirect cap: count=%d err=%v", count, err)
	}
}

func TestDownloadFilePresignedStorage(t *testing.T) {
	oldURL, oldKey, oldE2E := gokapiUrl, apiKey, e2eKey
	defer func() { gokapiUrl, apiKey, e2eKey = oldURL, oldKey, oldE2E }()
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("apikey") != "" {
			t.Error("download leaked apikey")
		}
		if r.URL.Query().Get("signature") != "synthetic" {
			t.Error("presigned query lost")
		}
		io.WriteString(w, "downloaded")
	}))
	defer storage.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("apikey") != "synthetic" {
			t.Error("API missing key")
		}
		if strings.Contains(r.URL.Path, "/list/") {
			json.NewEncoder(w).Encode(models.FileApiOutput{Name: "result.txt", SizeBytes: 10})
			return
		}
		http.Redirect(w, r, storage.URL+"/object?signature=synthetic", 307)
	}))
	defer api.Close()
	Init(api.URL, "synthetic", nil)
	dir := t.TempDir()
	if err := DownloadFile(cliflags.FlagConfig{DownloadId: "id", OutputPath: dir, JsonOutput: true}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "result.txt"))
	if err != nil || string(b) != "downloaded" {
		t.Fatalf("file mismatch: %v", err)
	}
}

func TestAPIEntryPointsRejectCrossOrigin(t *testing.T) {
	oldURL, oldKey := gokapiUrl, apiKey
	defer func() { gokapiUrl, apiKey = oldURL, oldKey }()
	for _, code := range []int{307, 308} {
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("API entry point leaked request to storage") }))
		defer target.Close()
		source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, code) }))
		defer source.Close()
		Init(source.URL, "synthetic", nil)
		if _, err := getUrl(gokapiUrl+"/chunk/complete", []header{{"password", "synthetic-password"}}, false); err == nil {
			t.Error("getUrl accepted cross-origin")
		}
		if _, err := completeChunk("synthetic-uuid", "synthetic.txt", 14, 14, false,
			cliflags.FlagConfig{Password: "synthetic-password", JsonOutput: true}, nil); err == nil {
			t.Error("completeChunk accepted cross-origin")
		}
		if err := uploadChunk(strings.NewReader("synthetic-file"), "synthetic-uuid", 0, 14, 14, nil); err == nil {
			t.Error("uploadChunk accepted cross-origin")
		}
		if err := setE2eInfo(models.E2EInfoEncrypted{}); err == nil {
			t.Error("setE2eInfo accepted cross-origin")
		}
	}
}

func TestSameOriginPostAndTimeout(t *testing.T) {
	for _, timeout := range []time.Duration{0, 30 * time.Second, 30 * time.Minute} {
		if newHTTPClient(timeout, false).Timeout != timeout {
			t.Error("timeout changed")
		}
	}
	for _, code := range []int{307, 308} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/start" {
				http.Redirect(w, r, "/finish", code)
				return
			}
			b, _ := io.ReadAll(r.Body)
			if r.Method != "POST" || string(b) != "synthetic-body" || r.Header.Get("apikey") != "synthetic" {
				t.Error("same-origin POST changed")
			}
		}))
		defer srv.Close()
		req, _ := http.NewRequest("POST", srv.URL+"/start", strings.NewReader("synthetic-body"))
		req.Header.Set("apikey", "synthetic")
		res, err := newHTTPClient(0, false).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
	}
}

func TestRedirectHistoryCannotRestoreSecrets(t *testing.T) {
	first, _ := http.NewRequest("GET", "https://api.test/start", nil)
	storage, _ := http.NewRequest("GET", "https://storage.test/object", nil)
	for _, target := range []string{"https://storage.test/next", "https://third.test/object", "https://api.test/return"} {
		next, _ := http.NewRequest("GET", target, nil)
		next.Header.Set("apikey", "synthetic")
		next.Header.Set("password", "synthetic")
		if err := newHTTPClient(0, true).CheckRedirect(next, []*http.Request{first, storage}); err != nil {
			t.Fatal(err)
		}
		if len(next.Header) != 0 {
			t.Fatal("credentials restored after trust boundary")
		}
	}
	next, _ := http.NewRequest("GET", "http://third.test/object", nil)
	if err := newHTTPClient(0, true).CheckRedirect(next, []*http.Request{first, storage}); err == nil {
		t.Fatal("later-hop downgrade accepted")
	}
}
