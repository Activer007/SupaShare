package logging

import (
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/test"
	"github.com/Activer007/SupaShare/internal/test/testconfiguration"
)

func TestMain(m *testing.M) {
	testconfiguration.Create(false)
	exitVal := m.Run()
	testconfiguration.Delete()
	os.Exit(exitVal)
}

func TestGetIpAddress(t *testing.T) {
	Init("test")
	r := httptest.NewRequest("GET", "/test", nil)
	test.IsEqualString(t, GetIpAddress(r), "192.0.2.1")
	r = httptest.NewRequest("GET", "/test", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	test.IsEqualString(t, GetIpAddress(r), "127.0.0.1")
	r.RemoteAddr = "invalid"
	test.IsEqualString(t, GetIpAddress(r), "invalid")
	r.Header.Add("X-REAL-IP", "invalid")
	test.IsEqualString(t, GetIpAddress(r), "invalid")
	r.Header.Add("X-FORWARDED-FOR", "invalid")
	test.IsEqualString(t, GetIpAddress(r), "invalid")
	r.RemoteAddr = "127.0.0.1"
	r.Header.Del("X-REAL-IP")
	r.Header.Del("X-FORWARDED-FOR")
	test.IsEqualString(t, GetIpAddress(r), "127.0.0.1")
	r.Header.Add("X-REAL-IP", "1.1.1.1")
	test.IsEqualString(t, GetIpAddress(r), "1.1.1.1")
	r.Header.Add("X-FORWARDED-FOR", "1.1.1.1, 2.2.2.2")
	test.IsEqualString(t, GetIpAddress(r), "2.2.2.2")
	useCloudflare = true
	r.Header.Add("CF-Connecting-IP", "3.3.3.3")
	test.IsEqualString(t, GetIpAddress(r), "3.3.3.3")
}

func TestInit(t *testing.T) {
	Init("test")
	test.IsEqualString(t, logPath, "test/log.txt")
}

func TestAddString(t *testing.T) {
	test.FileDoesNotExist(t, "test/log.txt")
	createLogEntry(categoryInfo, "Hello", true)
	test.FileExists(t, "test/log.txt")
	content, _ := os.ReadFile("test/log.txt")
	test.IsEqualBool(t, strings.Contains(string(content), "UTC   [info] Hello"), true)
}

func TestAddDownload(t *testing.T) {
	file := models.File{
		Id:   "testId",
		Name: "testName",
	}
	r := httptest.NewRequest("GET", "/test", nil)
	r.RemoteAddr = "127.0.0.1"
	r.Header.Set("User-Agent", "testAgent")
	r.Header.Add("X-REAL-IP", "1.1.1.1")
	LogDownload(file, r, true)
	// Need sleep, as LogDownload() is non-blocking
	time.Sleep(500 * time.Millisecond)
	content, _ := os.ReadFile("test/log.txt")
	test.IsEqualBool(t, strings.Contains(string(content), "UTC   [download] testName, IP 1.1.1.1, ID testId, Useragent testAgent"), true)
	r.Header.Add("X-REAL-IP", "2.2.2.2")
	LogDownload(file, r, false)
	// Need sleep, as LogDownload() is non-blocking
	time.Sleep(500 * time.Millisecond)
	content, _ = os.ReadFile("test/log.txt")
	test.IsEqualBool(t, strings.Contains(string(content), "2.2.2.2"), false)
}

func TestWriteToFileResilience(t *testing.T) {
	origPath := logPath
	defer func() { logPath = origPath }()

	// Point logPath to an invalid location where file creation is guaranteed to fail
	logPath = "/nonexistent_dir/cannot_create/log.txt"

	// writeToFile must not panic even when file open/write fails
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("writeToFile panicked on I/O error: %v", r)
		}
	}()

	writeToFile("this should fail gracefully without crashing")
	deleteAllLogs("testUser", 1, httptest.NewRequest("GET", "/", nil))
	DeleteLogs("testUser", 1, time.Now().Unix(), httptest.NewRequest("GET", "/", nil))
	_, _ = GetAll()
	_ = GetSince(time.Now().Unix())
}

func TestCheckUntrustedForwardingHeader(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "198.51.100.1:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.1")

	ip := GetIpAddress(req)
	test.IsEqualString(t, ip, "198.51.100.1")
}
