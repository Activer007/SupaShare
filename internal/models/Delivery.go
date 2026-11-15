package models

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Delivery organizes existing ordinary shares. It never owns their blobs.
type Delivery struct {
	Token     string   `json:"token"`
	OwnerID   int      `json:"ownerId"`
	Title     string   `json:"title"`
	Note      string   `json:"note"`
	FileIDs   []string `json:"fileIds"`
	CreatedAt int64    `json:"createdAt"`
	ExpiresAt int64    `json:"expiresAt"`
	RevokedAt int64    `json:"revokedAt"`
}

const MaxDeliveryFiles = 20
const MaxDeliveriesPerOwner = 100
const MaxDeliveriesGlobal = 10000
const DeliveryRetentionSeconds int64 = 7 * 86400
const DeliveryDefaultLifetime int64 = 30 * 86400
const DeliveryMaxLifetime int64 = 90 * 86400

var ErrDeliveryQuota = errors.New("delivery capacity reached")

// ValidDeliveryToken checks the public capability's URL-safe representation.
// The creation service is responsible for generating at least 128 random bits.
func ValidDeliveryToken(token string) bool {
	if len(token) < 22 || len(token) > 128 {
		return false
	}
	for _, c := range token {
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// Valid checks bounded persistent structure; ownership and current file state
// must additionally be verified by the service before creating a delivery.
func (d Delivery) Valid() bool {
	if !ValidDeliveryToken(d.Token) || d.OwnerID <= 0 || strings.TrimSpace(d.Title) == "" ||
		!utf8.ValidString(d.Title) || utf8.RuneCountInString(d.Title) > 200 ||
		!utf8.ValidString(d.Note) || utf8.RuneCountInString(d.Note) > 2000 ||
		len(d.FileIDs) < 2 || len(d.FileIDs) > MaxDeliveryFiles || d.CreatedAt <= 0 ||
		d.ExpiresAt < 0 || d.RevokedAt < 0 {
		return false
	}
	seen := make(map[string]bool, len(d.FileIDs))
	for _, id := range d.FileIDs {
		if id == "" || len(id) > 256 || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}

// EncodeDeliveryCursor preserves chronological keyset order without server state.
func EncodeDeliveryCursor(d Delivery) string { return fmt.Sprintf("%020d.%s", d.CreatedAt, d.Token) }
func DecodeDeliveryCursor(cursor string) (int64, string, error) {
	if len(cursor) < 43 || len(cursor) > 149 || cursor[20] != '.' {
		return 0, "", errors.New("invalid delivery cursor")
	}
	n, err := strconv.ParseInt(cursor[:20], 10, 64)
	token := cursor[21:]
	if err != nil || n <= 0 || !ValidDeliveryToken(token) || fmt.Sprintf("%020d", n) != cursor[:20] {
		return 0, "", errors.New("invalid delivery cursor")
	}
	return n, token, nil
}
