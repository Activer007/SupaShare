package models

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const MaxFileRequestsPerOwner = 100
const MaxFileRequestsGlobal = 10000
const FileRequestRetentionSeconds int64 = 7 * 86400
const FileRequestDefaultLifetime int64 = 5 * 86400
const FileRequestMaxLifetime int64 = 90 * 86400

var ErrFileRequestQuota = errors.New("file request capacity reached")

func ValidFileRequestID(id string) bool {
	if len(id) < 1 || len(id) > 128 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.') {
			return false
		}
	}
	return true
}
func FileRequestCursor(r FileRequest) string { return fmt.Sprintf("%020d.%s", r.CreationDate, r.Id) }
func ParseFileRequestCursor(s string) (int64, string, error) {
	if len(s) < 22 || len(s) > 149 || s[20] != '.' {
		return 0, "", errors.New("invalid file request cursor")
	}
	n, e := strconv.ParseInt(s[:20], 10, 64)
	id := s[21:]
	if e != nil || n < 0 || !ValidFileRequestID(id) || fmt.Sprintf("%020d", n) != s[:20] {
		return 0, "", errors.New("invalid file request cursor")
	}
	return n, id, nil
}
func (r FileRequest) ValidLifecycle() bool {
	return ValidFileRequestID(r.Id) && r.UserId > 0 && r.CreationDate >= 0 && r.Expiry >= 0 && len(r.Name) <= 800 && len(r.Notes) <= 8000 && len(r.ApiKey) > 0 && len(r.ApiKey) <= 128 && !strings.ContainsAny(r.ApiKey, "\x00\r\n")
}
