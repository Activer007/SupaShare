package cliapi

import (
	"encoding/json"
	"github.com/Activer007/SupaShare/cmd/cli-uploader/cliflags"
	"github.com/Activer007/SupaShare/internal/encryption"
	"github.com/Activer007/SupaShare/internal/models"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDownloadLegacyUnknownSize(t *testing.T) {
	oldURL, oldKey, oldE2E := gokapiUrl, apiKey, e2eKey
	defer func() { gokapiUrl, apiKey, e2eKey = oldURL, oldKey, oldE2E }()
	for _, tc := range []struct {
		name          string
		info          models.FileApiOutput
		contentLength bool
		limit         int64
		pass          bool
	}{
		{name: "legacy-header", contentLength: true, limit: 3, pass: true},
		{name: "legacy-stream", limit: 3, pass: true},
		{name: "legacy-header-over-cap", contentLength: true, limit: 2},
		{name: "legacy-stream-over-cap", limit: 2},
		{name: "encrypted-stays-strict", info: models.FileApiOutput{IsEncrypted: true}, limit: 3},
		{name: "collection-stays-strict", info: models.FileApiOutput{IsFileRequest: true}, limit: 3},
		{name: "collection-id-stays-strict", info: models.FileApiOutput{FileRequestId: "request"}, limit: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var deleted atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.Contains(r.URL.Path, "/list/"):
					info := tc.info
					info.Name = "legacy.bin"
					_ = json.NewEncoder(w).Encode(info)
				case strings.Contains(r.URL.Path, "/delete"):
					deleted.Store(true)
				default:
					if tc.contentLength {
						w.Header().Set("Content-Length", "3")
					} else {
						w.(http.Flusher).Flush()
					}
					_, _ = io.WriteString(w, "abc")
				}
			}))
			defer server.Close()
			if err := Init(server.URL, "synthetic", nil); err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			err := DownloadFile(cliflags.FlagConfig{DownloadId: "legacy", OutputPath: dir,
				JsonOutput: true, RemoveRemote: true, MaxDownloadBytes: tc.limit})
			if (err == nil) != tc.pass || deleted.Load() != tc.pass {
				t.Fatalf("success=%v deleted=%v err=%v", tc.pass, deleted.Load(), err)
			}
			data, readErr := os.ReadFile(filepath.Join(dir, "legacy.bin"))
			if readErr != nil || int64(len(data)) > tc.limit || tc.pass && string(data) != "abc" {
				t.Fatal("legacy output exceeded its limit or lost content", readErr)
			}
		})
	}
}

func TestDownloadSizeAndExclusiveOutput(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		declared                int64
		data                    string
		cap                     int64
		existing, symlink, pass bool
	}{
		{"exact", 3, "abc", 3, false, false, true}, {"short", 4, "abc", 4, false, false, false},
		{"over", 2, "abc", 5, false, false, false}, {"budget", 4, "abcd", 3, false, false, false},
		{"existing", 3, "abc", 3, true, false, false}, {"symlink", 3, "abc", 3, false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			oldURL, oldKey, oldE2E := gokapiUrl, apiKey, e2eKey
			defer func() { gokapiUrl, apiKey, e2eKey = oldURL, oldKey, oldE2E }()
			dir := t.TempDir()
			target := filepath.Join(dir, "result")
			victim := filepath.Join(dir, "victim")
			if tc.existing {
				os.WriteFile(target, []byte("original"), 0600)
			}
			if tc.symlink {
				os.WriteFile(victim, []byte("original"), 0600)
				if err := os.Symlink(victim, target); err != nil {
					t.Skip(err)
				}
			}
			deleted := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.Contains(r.URL.Path, "/list/"):
					json.NewEncoder(w).Encode(models.FileApiOutput{Name: "result", SizeBytes: tc.declared})
				case strings.Contains(r.URL.Path, "/delete"):
					deleted = true
				default:
					w.(http.Flusher).Flush()
					io.WriteString(w, tc.data)
				}
			}))
			defer server.Close()
			if err := Init(server.URL, "test", nil); err != nil {
				t.Fatal(err)
			}
			err := DownloadFile(cliflags.FlagConfig{DownloadId: "id", OutputPath: dir, JsonOutput: true, RemoveRemote: true, MaxDownloadBytes: tc.cap})
			if (err == nil) != tc.pass || deleted != tc.pass {
				t.Fatalf("success=%v deleted=%v err=%v", tc.pass, deleted, err)
			}
			if tc.existing || tc.symlink {
				b, _ := os.ReadFile(target)
				if string(b) != "original" {
					t.Fatal("existing target changed")
				}
			}
			if tc.pass {
				b, _ := os.ReadFile(target)
				if string(b) != tc.data {
					t.Fatal("bad output")
				}
			}
		})
	}
}
func TestDownloadDecryptionAuthenticatesAndCountsWire(t *testing.T) {
	key, err := encryption.GetRandomCipher()
	if err != nil {
		t.Fatal(err)
	}
	enc, err := encryption.GetEncryptReader(key, strings.NewReader("secret"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(enc)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []bool{false, true} {
		input := append([]byte(nil), data...)
		if bad {
			input[len(input)-1] ^= 1
		}
		wire := &downloadReader{r: strings.NewReader(string(input)), remaining: int64(len(input))}
		dec, err := encryption.GetDecryptReader(key, wire)
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.ReadAll(&downloadReader{r: dec, remaining: 100})
		if (err != nil) != bad {
			t.Fatalf("auth result: %v", err)
		}
		if !bad && wire.remaining != 0 {
			t.Fatal("wire not consumed")
		}
	}
}

func TestDownloadFailureDoesNotRemoveReplacement(t *testing.T) {
	oldURL, oldKey, oldE2E := gokapiUrl, apiKey, e2eKey
	defer func() { gokapiUrl, apiKey, e2eKey = oldURL, oldKey, oldE2E }()
	dir := t.TempDir()
	target := filepath.Join(dir, "result")
	moved := filepath.Join(dir, "partial")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/list/") {
			json.NewEncoder(w).Encode(models.FileApiOutput{Name: "result", SizeBytes: 3})
			return
		}
		if err := os.Rename(target, moved); err != nil {
			t.Error(err)
		}
		if err := os.WriteFile(target, []byte("replacement"), 0600); err != nil {
			t.Error(err)
		}
		w.WriteHeader(500)
	}))
	defer srv.Close()
	Init(srv.URL, "synthetic", nil)
	if err := DownloadFile(cliflags.FlagConfig{DownloadId: "id", OutputPath: dir, JsonOutput: true}); err == nil {
		t.Fatal("wanted failure")
	}
	b, err := os.ReadFile(target)
	if err != nil || string(b) != "replacement" {
		t.Fatal("replacement was removed/changed")
	}
}
func TestInvalidInitClearsCredentials(t *testing.T) {
	oldURL, oldKey, oldE2E := gokapiUrl, apiKey, e2eKey
	defer func() { gokapiUrl, apiKey, e2eKey = oldURL, oldKey, oldE2E }()
	if err := Init("https://trusted.example", "synthetic", nil); err != nil {
		t.Fatal(err)
	}
	if err := Init("https://trusted.example@evil.example", "synthetic", nil); err == nil {
		t.Fatal("userinfo accepted")
	}
	if apiKey != "" || gokapiUrl != "" {
		t.Fatal("stale credential state")
	}
}
