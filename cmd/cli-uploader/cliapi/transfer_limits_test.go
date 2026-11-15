package cliapi

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestN09TransferConfigBounds(t *testing.T) {
	for _, c := range [][2]int{{1, 0}, {1, -1}, {1, 65}, {0, 45}, {-1, 45}, {int(math.MaxInt64/megaByte) + 1, 45}} {
		if validateTransferConfig(c[0], c[1]) == nil {
			t.Fatalf("accepted %v", c)
		}
	}
	if err := validateTransferConfig(1024, 45); err != nil {
		t.Fatal(err)
	}
}

func TestN09StreamingMultipart(t *testing.T) {
	source := strings.NewReader("abcdefgh")
	body, typ, length, err := multipartChunk(source, "uuid", 5, 3, 8)
	if err != nil {
		t.Fatal(err)
	}
	if source.Len() != 8 {
		t.Fatal("eager file read")
	}
	b, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(b)) != length {
		t.Fatal("incorrect content length")
	}
	_, params, _ := mime.ParseMediaType(typ)
	reader := multipart.NewReader(bytes.NewReader(b), params["boundary"])
	p, err := reader.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(p)
	if string(data) != "abc" {
		t.Fatalf("body=%q", data)
	}
	fields := map[string]string{}
	for {
		p, err = reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, _ = io.ReadAll(p)
		fields[p.FormName()] = string(data)
	}
	if fields["filesize"] != "8" || fields["offset"] != "5" || fields["uuid"] != "uuid" {
		t.Fatal(fields)
	}
	if source.Len() != 5 {
		t.Fatal("overread source")
	}
}

func TestN09BoundedResponses(t *testing.T) {
	if _, err := readBoundedBody(strings.NewReader("12345"), 4); err == nil {
		t.Fatal("oversize accepted")
	}
	if b, err := readBoundedBody(strings.NewReader("1234"), 4); err != nil || string(b) != "1234" {
		t.Fatal(err)
	}
	old := gokapiUrl
	defer func() { gokapiUrl = old }()
	for _, mode := range []string{"metadata", "chunk", "config"} {
		t.Run(mode, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "metadata":
					io.CopyN(w, strings.NewReader(strings.Repeat("x", int(maxMetadataBytes+1))), maxMetadataBytes+1)
				case "chunk":
					w.WriteHeader(500)
					io.WriteString(w, "\x1b[2Jremote marker")
				case "config":
					json.NewEncoder(w).Encode(map[string]int{"MaxFilesize": 1, "MaxChunksize": 0})
				}
			}))
			defer srv.Close()
			gokapiUrl = srv.URL
			switch mode {
			case "metadata":
				if _, err := getUrl(srv.URL, nil, false); err == nil {
					t.Fatal("oversize accepted")
				}
			case "chunk":
				err := uploadChunk(strings.NewReader("x"), "uuid", 0, 1, 1, nil)
				if err == nil || strings.Contains(err.Error(), "marker") || strings.Contains(err.Error(), "\x1b") {
					t.Fatalf("unsafe error %v", err)
				}
			case "config":
				if _, _, _, err := GetConfig(); err == nil {
					t.Fatal("bad config accepted")
				}
			}
		})
	}
}

func TestN09UploadFinalChunkAndShortSource(t *testing.T) {
	old := gokapiUrl
	defer func() { gokapiUrl = old }()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reader, err := r.MultipartReader()
		if err != nil {
			w.WriteHeader(400)
			return
		}
		p, err := reader.NextPart()
		if err != nil {
			w.WriteHeader(400)
			return
		}
		data, err := io.ReadAll(p)
		if err != nil || string(data) != "xyz" {
			w.WriteHeader(400)
			return
		}
		// Read the whole multipart stream so truncated ContentLength cannot pass.
		for {
			p, err = reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				w.WriteHeader(400)
				return
			}
			if _, err = io.Copy(io.Discard, p); err != nil {
				w.WriteHeader(400)
				return
			}
		}
		io.WriteString(w, `{"result":"OK"}`)
	}))
	defer srv.Close()
	gokapiUrl = srv.URL
	if err := uploadChunk(strings.NewReader("xyz"), "u", 5, 45*megaByte, 8, nil); err != nil {
		t.Fatal(err)
	}
	if err := uploadChunk(strings.NewReader("xy"), "u", 5, 45*megaByte, 8, nil); err == nil {
		t.Fatal("short source accepted")
	}
	for _, bounds := range [][3]int64{{0, 0, 1}, {0, -1, 1}, {0, maxChunkBytes + 1, 1}, {-1, 1, 1}, {1, 1, 1}} {
		if err := uploadChunk(strings.NewReader("x"), "u", bounds[0], bounds[1], bounds[2], nil); err == nil {
			t.Fatal("bad bounds accepted")
		}
	}
}
