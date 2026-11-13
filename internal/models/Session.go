package models

// Session contains cookie parameter
type Session struct {
	// Successor marks a revoked predecessor retained only for logout lineage.
	Successor  string `redis:"successor"`
	RenewAt    int64  `redis:"renew_at"`
	ValidUntil int64  `redis:"valid_until"`
	UserId     int    `redis:"user_id"`
}
