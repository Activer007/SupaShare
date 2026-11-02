//go:build test && !integration

package webserver

import (
	"bytes"
	"html"
	"image/png"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/image/webp"
)

func TestUIBranding(t *testing.T) {
	for _, name := range []string{"SupaShare", `<Team & "Files">`} {
		for _, page := range []string{"login", "admin"} {
			var view interface{ setUI(pageUI) }
			href := "./index"
			if page == "login" {
				view = &LoginView{PublicName: name}
			} else {
				view = &AdminView{PublicName: name, IsAdminView: true}
				href = "./admin"
			}
			response := httptest.NewRecorder()
			if err := renderPage(response, httptest.NewRequest("GET", "/"+page+"?lang=zh-CN", nil), page, view); err != nil {
				t.Fatal(err)
			}
			body := response.Body.String()
			if name == "SupaShare" {
				for _, want := range []string{`class="workspace-brand-image"`, `src="./assets/brand/supashare-logo-v1.webp"`, `alt="SupaShare"`, `href="` + href + `"`} {
					if !strings.Contains(body, want) {
						t.Errorf("%s missing brand contract %s", page, want)
					}
				}
			} else if strings.Contains(body, `class="workspace-brand-image"`) || !strings.Contains(body, html.EscapeString(name)) {
				t.Errorf("%s must preserve and escape the configured public name", page)
			}
			if !regexp.MustCompile(`href="\./favicon-32x32\.png\?v=[a-z0-9]+"`).MatchString(body) {
				t.Errorf("%s missing versioned favicon URL", page)
			}
		}
	}
	logo, err := staticFolderEmbedded.ReadFile("web/static/assets/brand/supashare-logo-v1.webp")
	if err != nil {
		t.Fatal(err)
	}
	size, err := webp.DecodeConfig(bytes.NewReader(logo))
	if err != nil || size.Width != 1000 || size.Height != 203 || len(logo) > 100*1024 {
		t.Fatalf("invalid or oversized embedded logo: %+v, %v", size, err)
	}
	icon, err := staticFolderEmbedded.ReadFile("web/static/defaultFavicon.png")
	if err != nil {
		t.Fatal(err)
	}
	size, err = png.DecodeConfig(bytes.NewReader(icon))
	if err != nil || size.Width != 512 || size.Height != 512 {
		t.Fatalf("favicon must satisfy the existing 512x512 PNG contract: %+v, %v", size, err)
	}
}
