package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"unicode"

	"github.com/Activer007/SupaShare/internal/models"
)

func TestUploadResultHumanSafeMachineOriginal(t *testing.T) {
	result := models.FileApiOutput{Name: "report\x1b]0;spoof\a\r.pdf", Id: "id\u009b31m", UrlDownload: "https://host/" + strings.Repeat("x", 4096) + "\u202e"}
	var out bytes.Buffer
	writeUploadResult(&out, result, false)
	for _, r := range out.String() {
		if r != '\n' && (unicode.IsControl(r) || unicode.Is(unicode.Cf, r)) {
			t.Fatal("untrusted terminal control rendered")
		}
	}
	if !strings.Contains(out.String(), "\\u001b") || !strings.Contains(out.String(), "[truncated]") {
		t.Fatal("missing visible escape or cap")
	}
	if len(out.String()) > 6200 {
		t.Fatal("human output unbounded")
	}
	out.Reset()
	writeUploadResult(&out, result, true)
	var decoded models.FileApiOutput
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Name != result.Name || decoded.Id != result.Id || decoded.UrlDownload != result.UrlDownload {
		t.Fatal("JSON metadata changed by display filtering")
	}
}
