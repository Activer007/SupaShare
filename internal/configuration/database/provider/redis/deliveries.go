package redis

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"

	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/uploadquota"
	redigo "github.com/gomodule/redigo/redis"
)

const keyDeliveries = "delivery:records"
const keyDeliveriesVersion = "delivery:version"
const maxDeliveryRecordBytes = 24000

func (p DatabaseProvider) initDeliveries() error {
	conn := p.pool.Get()
	defer conn.Close()
	if _, err := conn.Do("SETNX", p.dbPrefix+keyDeliveriesVersion, 1); err != nil {
		return err
	}
	if err := p.checkDeliverySchema(context.Background(), conn); err != nil {
		return err
	}
	return p.initDeliveryIndexes(context.Background(), conn)
}

func (p DatabaseProvider) checkDeliverySchema(ctx context.Context, conn redigo.Conn) error {
	version, err := redigo.Int(redigo.DoContext(conn, ctx, "GET", p.dbPrefix+keyDeliveriesVersion))
	if errors.Is(err, redigo.ErrNil) {
		// Only a genuinely absent legacy structure is empty. A missing
		// marker beside existing records is corruption, not permission to
		// silently drop delivery capabilities during offline migration.
		exists, readErr := redigo.Bool(redigo.DoContext(conn, ctx, "EXISTS", p.dbPrefix+keyDeliveries))
		if readErr != nil {
			return readErr
		}
		if exists {
			return uploadquota.ErrSchema
		}
		return errors.Join(uploadquota.ErrSchema, uploadquota.ErrNotFound)
	}
	if err != nil {
		return err
	}
	if version != 1 {
		return uploadquota.ErrSchema
	}
	return nil
}

func (p DatabaseProvider) deliveryConnection(ctx context.Context) (redigo.Conn, error) {
	if ctx == nil {
		return nil, uploadquota.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p.pool == nil {
		return nil, uploadquota.ErrSchema
	}
	conn, err := p.pool.GetContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := p.checkDeliverySchema(ctx, conn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func decodeDelivery(encoded []byte, token string) (models.Delivery, error) {
	var d models.Delivery
	if len(encoded) > maxDeliveryRecordBytes || json.Unmarshal(encoded, &d) != nil || !d.Valid() || d.Token != token {
		return models.Delivery{}, uploadquota.ErrInvalid
	}
	return d, nil
}

func (p DatabaseProvider) InsertDelivery(ctx context.Context, d models.Delivery) (bool, error) {
	if !d.Valid() {
		return false, uploadquota.ErrInvalid
	}
	conn, err := p.deliveryConnection(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	encoded, err := json.Marshal(d)
	if err != nil || len(encoded) > maxDeliveryRecordBytes {
		return false, uploadquota.ErrInvalid
	}
	return p.insertDelivery(ctx, conn, d, encoded, false)
}

func (p DatabaseProvider) ReadDelivery(ctx context.Context, token string) (models.Delivery, bool, error) {
	if !models.ValidDeliveryToken(token) {
		return models.Delivery{}, false, uploadquota.ErrInvalid
	}
	conn, err := p.onlineDeliveryConnection(ctx)
	if err != nil {
		return models.Delivery{}, false, err
	}
	defer conn.Close()
	encoded, err := redigo.Bytes(redigo.DoContext(conn, ctx, "HGET", p.dbPrefix+keyDeliveries, token))
	if errors.Is(err, redigo.ErrNil) {
		return models.Delivery{}, false, nil
	}
	if err != nil {
		return models.Delivery{}, false, err
	}
	d, err := decodeDelivery(encoded, token)
	return d, err == nil, err
}

func (p DatabaseProvider) ListDeliveries(ctx context.Context, ownerID int) ([]models.Delivery, error) {
	if ownerID <= 0 {
		return nil, uploadquota.ErrInvalid
	}
	return p.readDeliveries(ctx, ownerID)
}

// ReadAllDeliveries is for an offline migration of every owner's records.
func (p DatabaseProvider) ReadAllDeliveries(ctx context.Context) ([]models.Delivery, error) {
	return p.readDeliveries(ctx, 0)
}

func (p DatabaseProvider) readDeliveries(ctx context.Context, ownerID int) ([]models.Delivery, error) {
	conn, err := p.deliveryConnection(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	byToken := make(map[string]models.Delivery)
	cursor := "0"
	for {
		reply, err := redigo.Values(redigo.DoContext(conn, ctx, "HSCAN", p.dbPrefix+keyDeliveries, cursor, "COUNT", 100))
		if err != nil {
			return nil, err
		}
		var fields [][]byte
		if _, err := redigo.Scan(reply, &cursor, &fields); err != nil || len(fields)%2 != 0 {
			return nil, uploadquota.ErrInvalid
		}
		for i := 0; i < len(fields); i += 2 {
			d, err := decodeDelivery(fields[i+1], string(fields[i]))
			if err != nil {
				return nil, err
			}
			if ownerID == 0 || ownerID == d.OwnerID {
				byToken[d.Token] = d
			}
		}
		if cursor == "0" {
			break
		}
	}
	result := make([]models.Delivery, 0, len(byToken))
	for _, d := range byToken {
		result = append(result, d)
	}
	slices.SortFunc(result, func(a, b models.Delivery) int {
		return cmp.Or(cmp.Compare(b.CreatedAt, a.CreatedAt), cmp.Compare(a.Token, b.Token))
	})
	return result, nil
}

const revokeDeliveryScript = `
local raw = redis.call('HGET', KEYS[1], ARGV[1])
if not raw then return 0 end
local ok, d = pcall(cjson.decode, raw)
if not ok or type(d) ~= 'table' or type(d.ownerId) ~= 'number' or type(d.revokedAt) ~= 'number' then return -1 end
if d.ownerId ~= tonumber(ARGV[2]) then return 0 end
if d.revokedAt == 0 then
  d.revokedAt = tonumber(ARGV[3])
  redis.call('HSET', KEYS[1], ARGV[1], cjson.encode(d))
  local deadline = d.revokedAt
  if d.expiresAt > 0 and d.expiresAt < deadline then deadline = d.expiresAt end
  redis.call('ZADD', KEYS[2], deadline + tonumber(ARGV[4]), ARGV[1])
end
return 1`

func (p DatabaseProvider) RevokeDelivery(ctx context.Context, token string, ownerID int, now int64) (bool, error) {
	if !models.ValidDeliveryToken(token) || ownerID <= 0 || now <= 0 {
		return false, uploadquota.ErrInvalid
	}
	conn, err := p.onlineDeliveryConnection(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	result, err := redigo.Int(redigo.DoContext(conn, ctx, "EVAL", revokeDeliveryScript, 2, p.dbPrefix+keyDeliveries, p.dbPrefix+keyDeliveryExpiry, token, ownerID, now, models.DeliveryRetentionSeconds))
	if err != nil {
		return false, err
	}
	if result == -1 {
		return false, uploadquota.ErrInvalid
	}
	return result == 1, nil
}

const keyDeliveryOwners = "delivery:owner:"
const keyDeliveryExpiry = "delivery:cleanup"
const keyDeliveryIndexes = "delivery:indexes:v1"

// This startup-only migration is resumable. Each record is re-read atomically
// before indexing, so another upgraded instance may safely insert or prune.
// Stop old writers before upgrade: they do not maintain the new indexes.
func (p DatabaseProvider) initDeliveryIndexes(ctx context.Context, conn redigo.Conn) error {
	version, err := redigo.Int(redigo.DoContext(conn, ctx, "GET", p.dbPrefix+keyDeliveryIndexes))
	if err == nil {
		if version != 1 {
			return uploadquota.ErrSchema
		}
		return nil
	}
	if !errors.Is(err, redigo.ErrNil) {
		return err
	}
	cursor := "0"
	for {
		reply, err := redigo.Values(redigo.DoContext(conn, ctx, "HSCAN", p.dbPrefix+keyDeliveries, cursor, "COUNT", 100))
		if err != nil {
			return err
		}
		var fields [][]byte
		if _, err = redigo.Scan(reply, &cursor, &fields); err != nil {
			return err
		}
		if len(fields)%2 != 0 {
			return uploadquota.ErrInvalid
		}
		for i := 0; i < len(fields); i += 2 {
			d, err := decodeDelivery(fields[i+1], string(fields[i]))
			if err != nil {
				return err
			}
			_, err = redigo.DoContext(conn, ctx, "EVAL", indexDeliveryScript, 3, p.dbPrefix+keyDeliveries, p.dbPrefix+keyDeliveryOwners+strconv.Itoa(d.OwnerID), p.dbPrefix+keyDeliveryExpiry, d.Token, fields[i+1], models.DeliveryRetentionSeconds)
			if err != nil {
				return err
			}
		}
		if cursor == "0" {
			break
		}
	}
	_, err = redigo.DoContext(conn, ctx, "SET", p.dbPrefix+keyDeliveryIndexes, 1)
	return err
}

const indexDeliveryScript = `
local raw = redis.call('HGET', KEYS[1], ARGV[1])
if not raw then return 0 end
local d = cjson.decode(raw)
redis.call('ZADD', KEYS[2], 0, string.format('%020d', d.createdAt) .. '.' .. ARGV[1])
local deadline = d.expiresAt
if d.revokedAt > 0 and (deadline == 0 or d.revokedAt < deadline) then deadline = d.revokedAt end
if deadline > 0 then redis.call('ZADD', KEYS[3], deadline + tonumber(ARGV[3]), ARGV[1]) end
return 1`

const insertDeliveryScript = `
if redis.call('HEXISTS', KEYS[1], ARGV[1]) == 1 then return 0 end
if ARGV[4] == '1' and (redis.call('HLEN', KEYS[1]) >= tonumber(ARGV[5]) or redis.call('ZCARD', KEYS[2]) >= tonumber(ARGV[6])) then return -1 end
local d = cjson.decode(ARGV[2])
redis.call('HSET', KEYS[1], ARGV[1], ARGV[2])
redis.call('ZADD', KEYS[2], 0, string.format('%020d', d.createdAt) .. '.' .. ARGV[1])
local deadline = d.expiresAt
if d.revokedAt > 0 and (deadline == 0 or d.revokedAt < deadline) then deadline = d.revokedAt end
if deadline > 0 then redis.call('ZADD', KEYS[3], deadline + tonumber(ARGV[3]), ARGV[1]) end
return 1`

func (p DatabaseProvider) insertDelivery(ctx context.Context, conn redigo.Conn, d models.Delivery, encoded []byte, limited bool) (bool, error) {
	mode := 0
	if limited {
		mode = 1
	}
	n, err := redigo.Int(redigo.DoContext(conn, ctx, "EVAL", insertDeliveryScript, 3, p.dbPrefix+keyDeliveries, p.dbPrefix+keyDeliveryOwners+strconv.Itoa(d.OwnerID), p.dbPrefix+keyDeliveryExpiry, d.Token, encoded, models.DeliveryRetentionSeconds, mode, models.MaxDeliveriesGlobal, models.MaxDeliveriesPerOwner))
	if err != nil {
		return false, err
	}
	if n == -1 {
		return false, models.ErrDeliveryQuota
	}
	return n == 1, nil
}

func (p DatabaseProvider) CreateDeliveryLimited(ctx context.Context, d models.Delivery, now int64) (bool, error) {
	if !d.Valid() || now <= 0 {
		return false, uploadquota.ErrInvalid
	}
	conn, err := p.onlineDeliveryConnection(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	encoded, err := json.Marshal(d)
	if err != nil || len(encoded) > maxDeliveryRecordBytes {
		return false, uploadquota.ErrInvalid
	}
	// A bounded cleanup can free quota without turning admission into an
	// unbounded scan. Retained revoked/expired records still consume quota.
	if _, err := p.pruneDeliveries(ctx, conn, now, 100); err != nil {
		return false, err
	}
	return p.insertDelivery(ctx, conn, d, encoded, true)
}

func (p DatabaseProvider) ListDeliveriesPage(ctx context.Context, ownerID int, beforeToken string, limit int) ([]models.Delivery, error) {
	if beforeToken != "" {
		if _, _, err := models.DecodeDeliveryCursor(beforeToken); err != nil {
			return nil, uploadquota.ErrInvalid
		}
	}
	if ownerID <= 0 || limit < 1 || limit > 101 {
		return nil, uploadquota.ErrInvalid
	}
	conn, err := p.onlineDeliveryConnection(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	upper := "+"
	if beforeToken != "" {
		upper = "(" + beforeToken
	}
	rawRows, err := redigo.ByteSlices(redigo.DoContext(conn, ctx, "EVAL", listDeliveryPageScript, 2, p.dbPrefix+keyDeliveryOwners+strconv.Itoa(ownerID), p.dbPrefix+keyDeliveries, upper, limit))
	if err != nil {
		return nil, err
	}
	if len(rawRows)%2 != 0 {
		return nil, uploadquota.ErrInvalid
	}
	out := make([]models.Delivery, 0, len(rawRows)/2)
	for i := 0; i < len(rawRows); i += 2 {
		d, err := decodeDelivery(rawRows[i+1], string(rawRows[i]))
		if err != nil {
			return nil, err
		}
		if d.OwnerID != ownerID {
			return nil, uploadquota.ErrInvalid
		}
		out = append(out, d)
	}
	return out, nil
}

// Return one bounded atomic page, so pruning cannot shorten it between
// selecting keys and reading payloads and accidentally hide a next page.
const listDeliveryPageScript = `
local tokens = redis.call('ZREVRANGEBYLEX', KEYS[1], ARGV[1], '-', 'LIMIT', 0, ARGV[2])
local result = {}
for _, member in ipairs(tokens) do
 local token = string.sub(member, 22)
 local raw = redis.call('HGET', KEYS[2], token)
 if not raw then return redis.error_reply('delivery owner index is inconsistent') end
 table.insert(result, token)
 table.insert(result, raw)
end
return result`

const pruneDeliveriesScript = `
local tokens = redis.call('ZRANGEBYSCORE', KEYS[2], '-inf', ARGV[1], 'LIMIT', 0, ARGV[2])
local deleted = 0
for _, token in ipairs(tokens) do
 local raw = redis.call('HGET', KEYS[1], token)
 if raw then
  local ok, d = pcall(cjson.decode, raw)
  if not ok or type(d) ~= 'table' or type(d.ownerId) ~= 'number' or type(d.expiresAt) ~= 'number' or type(d.revokedAt) ~= 'number' then return -1 end
  local deadline = d.expiresAt
  if d.revokedAt > 0 and (deadline == 0 or d.revokedAt < deadline) then deadline = d.revokedAt end
  if deadline > 0 and deadline + tonumber(ARGV[4]) <= tonumber(ARGV[1]) then
   redis.call('HDEL', KEYS[1], token)
   redis.call('ZREM', ARGV[3] .. tostring(d.ownerId), string.format('%020d', d.createdAt) .. '.' .. token)
   redis.call('ZREM', KEYS[2], token)
   deleted = deleted + 1
  else
   redis.call('ZREM', KEYS[2], token)
   if deadline > 0 then redis.call('ZADD', KEYS[2], deadline + tonumber(ARGV[4]), token) end
  end
 else redis.call('ZREM', KEYS[2], token) end
end
return deleted`

func (p DatabaseProvider) pruneDeliveries(ctx context.Context, conn redigo.Conn, now int64, limit int) (int, error) {
	n, err := redigo.Int(redigo.DoContext(conn, ctx, "EVAL", pruneDeliveriesScript, 2, p.dbPrefix+keyDeliveries, p.dbPrefix+keyDeliveryExpiry, now, limit, p.dbPrefix+keyDeliveryOwners, models.DeliveryRetentionSeconds))
	if err != nil {
		return 0, err
	}
	if n < 0 {
		return 0, uploadquota.ErrInvalid
	}
	return n, nil
}
func (p DatabaseProvider) PruneDeliveries(ctx context.Context, now int64, limit int) (int, error) {
	if now <= 0 || limit < 1 || limit > 1000 {
		return 0, uploadquota.ErrInvalid
	}
	conn, err := p.onlineDeliveryConnection(ctx)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	return p.pruneDeliveries(ctx, conn, now, limit)
}

// Legacy migration reads intentionally use deliveryConnection. Online paths
// fail closed when an upgrade was skipped or its completion marker is damaged.
func (p DatabaseProvider) onlineDeliveryConnection(ctx context.Context) (redigo.Conn, error) {
	conn, err := p.deliveryConnection(ctx)
	if err != nil {
		return nil, err
	}
	version, err := redigo.Int(redigo.DoContext(conn, ctx, "GET", p.dbPrefix+keyDeliveryIndexes))
	if err != nil || version != 1 {
		_ = conn.Close()
		return nil, uploadquota.ErrSchema
	}
	return conn, nil
}
