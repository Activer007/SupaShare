package uploadreceive

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReceiveProgressDeadlineRealHTTP(t *testing.T) {
	prefixes := map[string]string{
		"before-first-part": "",
		"multipart-header":  "--b\r\nContent-Disposition: form-data; name=\"file\"; filename=\"a\"",
		"field":             "--b\r\nContent-Disposition: form-data; name=\"name\"\r\n\r\nstalled",
		"payload":           "--b\r\nContent-Disposition: form-data; name=\"file\"; filename=\"a\"\r\n\r\npayload",
		"epilogue":          "--b\r\nContent-Disposition: form-data; name=\"file\"; filename=\"a\"\r\n\r\nx\r\n--b--\r\nepilogue",
	}
	for name, prefix := range prefixes {
		for _, chunked := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/chunked=%t", name, chunked), func(t *testing.T) {
				p, _ := New(4, 64<<20)
				p.progressWindow = 150 * time.Millisecond
				dir := t.TempDir()
				outcome := make(chan error, 1)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					u, err := p.ReceiveScoped(w, r, dir, 1<<20, 0, Scope{UserID: 1})
					if u != nil {
						_ = u.Close()
					}
					if errors.Is(err, ErrTimeout) {
						w.Header().Set("Connection", "close")
						http.Error(w, "timeout", http.StatusRequestTimeout)
					} else {
						http.Error(w, "unexpected", 500)
					}
					outcome <- err
				}))
				defer server.Close()
				conn, err := net.Dial("tcp", server.Listener.Addr().String())
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
				bodyHeader := "Content-Length: 100000\r\n"
				wire := prefix
				if chunked {
					bodyHeader = "Transfer-Encoding: chunked\r\n"
					if prefix != "" {
						wire = fmt.Sprintf("%x\r\n%s\r\n", len(prefix), prefix)
					}
				}
				started := time.Now()
				_, err = fmt.Fprintf(conn, "POST / HTTP/1.1\r\nHost: test\r\nContent-Type: multipart/form-data; boundary=b\r\n%s\r\n%s", bodyHeader, wire)
				if err != nil {
					t.Fatal(err)
				}
				response, err := http.ReadResponse(bufio.NewReader(conn), nil)
				if err != nil {
					t.Fatal(err)
				}
				response.Body.Close()
				if response.StatusCode != 408 || time.Since(started) > 2*time.Second {
					t.Fatalf("status=%d elapsed=%v", response.StatusCode, time.Since(started))
				}
				if err := <-outcome; !errors.Is(err, ErrTimeout) {
					t.Fatalf("error=%v", err)
				}
				p.mu.Lock()
				active, reserved, subjects := p.active, p.reserved, len(p.subjects)
				p.mu.Unlock()
				if active != 0 || reserved != 0 || subjects != 0 {
					t.Fatalf("leak active=%d reserved=%d subjects=%d", active, reserved, subjects)
				}
				entries, err := os.ReadDir(filepath.Join(dir, spoolDirectory))
				if err != nil {
					t.Fatal(err)
				}
				for _, e := range entries {
					if e.Name() != markerName {
						t.Fatalf("spool leak %s", e.Name())
					}
				}
				// A different uploader is admitted immediately after timeout.
				next, err := p.ReceiveScoped(httptest.NewRecorder(), receiveRequest(t, true, nil, []byte("ok"), true), dir, 100, 0, Scope{UserID: 2})
				if err != nil {
					t.Fatal(err)
				}
				_ = next.Close()
			})
		}
	}
}

func TestReceiveProgressTrickleDoesNotRenew(t *testing.T) {
	p, _ := New(4, 64<<20)
	p.progressWindow = 180 * time.Millisecond
	dir := t.TempDir()
	outcome := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, e := p.Receive(w, r, dir, 1<<20, 0)
		if u != nil {
			_ = u.Close()
		}
		outcome <- e
		w.Header().Set("Connection", "close")
		http.Error(w, "timeout", 408)
	}))
	defer server.Close()
	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	fmt.Fprint(conn, "POST / HTTP/1.1\r\nHost: test\r\nContent-Type: multipart/form-data; boundary=b\r\nContent-Length: 10000\r\n\r\n--b\r\n")
	for range 4 {
		time.Sleep(30 * time.Millisecond)
		_, _ = conn.Write([]byte("x"))
	}
	select {
	case e := <-outcome:
		if !errors.Is(e, ErrTimeout) {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("trickle renewed receive slot")
	}
}

func TestReceiveProgressContinuesAndClearsDeadline(t *testing.T) {
	p, _ := New(4, 64<<20)
	p.progressWindow = 300 * time.Millisecond
	p.progressBytes = 1024
	dir := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, e := p.ReceiveScoped(w, r, dir, 1<<20, 0, Scope{UserID: 1})
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		_ = u.Close()
		time.Sleep(350 * time.Millisecond)
		_, _ = io.WriteString(w, "ok")
	}))
	defer server.Close()
	conn, e := net.Dial("tcp", server.Listener.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	prefix := "--b\r\nContent-Disposition: form-data; name=\"file\"; filename=\"a\"\r\n\r\n"
	suffix := "\r\n--b--\r\n"
	fmt.Fprintf(conn, "POST / HTTP/1.1\r\nHost: test\r\nContent-Type: multipart/form-data; boundary=b\r\nContent-Length: %d\r\n\r\n%s", len(prefix)+6*2048+len(suffix), prefix)
	for range 6 {
		_, _ = conn.Write(bytes.Repeat([]byte("x"), 2048))
		time.Sleep(80 * time.Millisecond)
	}
	_, _ = io.WriteString(conn, suffix)
	res, e := http.ReadResponse(bufio.NewReader(conn), nil)
	if e != nil {
		t.Fatal(e)
	}
	data, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || string(data) != "ok" {
		t.Fatalf("%d %s", res.StatusCode, data)
	}
}

func TestReceiveScopedFairness(t *testing.T) {
	p, _ := New(4, 64<<20)
	dir := t.TempDir()
	receive := func(scope Scope) (*Upload, error) {
		return p.ReceiveScoped(httptest.NewRecorder(), receiveRequest(t, true, nil, []byte("ok"), true), dir, 100, 0, scope)
	}
	var opened []*Upload
	defer func() {
		for _, u := range opened {
			_ = u.Close()
		}
	}()
	for _, scope := range []Scope{{UserID: 1, RequestID: "a"}, {UserID: 1, RequestID: "b"}} {
		u, e := receive(scope)
		if e != nil {
			t.Fatal(e)
		}
		opened = append(opened, u)
	}
	if _, e := receive(Scope{UserID: 1, RequestID: "c"}); !errors.Is(e, ErrBusy) {
		t.Fatalf("same owner consumed all slots: %v", e)
	}
	for range 2 {
		u, e := receive(Scope{UserID: 2, RequestID: "other"})
		if e != nil {
			t.Fatal(e)
		}
		opened = append(opened, u)
	}
	if _, e := receive(Scope{UserID: 3, RequestID: "other"}); !errors.Is(e, ErrBusy) {
		t.Fatal(e)
	}
	for _, u := range opened {
		_ = u.Close()
	}
	if len(p.subjects) != 0 || p.active != 0 {
		t.Fatal("scope budget not released")
	}
	// Request ID cap is independent of the owner cap.
	for _, owner := range []int{1, 2} {
		u, e := receive(Scope{UserID: owner, RequestID: "shared"})
		if e != nil {
			t.Fatal(e)
		}
		opened = append(opened, u)
	}
	if _, e := receive(Scope{UserID: 3, RequestID: "shared"}); !errors.Is(e, ErrBusy) {
		t.Fatal(e)
	}
	if _, e := receive(Scope{UserID: 3, RequestID: strings.Repeat("x", 129)}); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
}

func TestReceiveProgressDeadlineHTTP2(t *testing.T) {
	p, _ := New(4, 64<<20)
	p.progressWindow = 150 * time.Millisecond
	dir := t.TempDir()
	outcome := make(chan error, 1)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 {
			t.Error("HTTP/2 was not exercised")
		}
		u, e := p.ReceiveScoped(w, r, dir, 1<<20, 0, Scope{UserID: 1})
		if u != nil {
			_ = u.Close()
		}
		outcome <- e
		http.Error(w, "timeout", 408)
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	req, e := http.NewRequest(http.MethodPost, server.URL, reader)
	if e != nil {
		t.Fatal(e)
	}
	req.Header.Set("Content-Type", "multipart/form-data; boundary=b")
	done := make(chan error, 1)
	go func() {
		_, e := io.WriteString(writer, "--b\r\nContent-Disposition: form-data; name=\"file\"; filename=\"x\"\r\n\r\nstalled")
		done <- e
	}()
	client := server.Client()
	client.Timeout = 3 * time.Second
	res, e := client.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 408 {
		t.Fatal(res.StatusCode)
	}
	if e := <-outcome; !errors.Is(e, ErrTimeout) {
		t.Fatal(e)
	}
	writer.Close()
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	if p.active != 0 || p.reserved != 0 || len(p.subjects) != 0 {
		t.Fatal("HTTP/2 receiver budget leak")
	}
}
