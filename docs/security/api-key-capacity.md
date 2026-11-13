# Ordinary API-key capacity

The `/api/auth/create` endpoint now admits ordinary API keys atomically in the configured database, including concurrent server instances. The API-key rename endpoint also bounds names.

## Limits and compatibility

- 50 stored ordinary keys per user, 10,000 per database namespace (including administrators).
- Friendly names: at most 256 UTF-8 **bytes**, not characters. Existing UTF-8/control-character validation still applies. Base64 header input is bounded before decoding and checked again after decoding.
- Temporary system authentication tokens and File Request credentials do not consume this quota. Reaching capacity therefore does not prevent obtaining the temporary token needed to manage/delete existing keys.
- Existing credentials are not revoked or truncated. Existing over-limit keys remain readable, usable and deletable. Oversized names may be changed to valid shorter names. Existing records are counted immediately; no counter bootstrap or schema migration is needed.
- SQLite conservatively counts stored expired ordinary records until its existing cleanup removes them. Redis expires records normally. New ordinary keys remain non-expiring, as before.
- A full quota returns HTTP 409 with an actionable capacity message and **no new credential**. Delete unused keys and retry; global capacity requires an administrator to remove unused keys across owners. Normal authorization still applies. No automatic/bulk deletion is introduced.
- These fixed generous bounds preserve ordinary development use without introducing new deployment settings or disabling user API-key creation. The existing instance option disabling the API menu is unchanged. A future deployment needing larger quotas should deliberately revise these centralized constants and reassess resource cost rather than silently bypass admission.

The count caps and bounded new string fields bound *new* ordinary-key storage. This does not remove legacy oversized records or establish a quota for system tokens/File Requests. Existing-key property edits use update-only writes, so a concurrent deletion cannot resurrect a key outside the admission gate. Database import/restore preserves existing data; they are not exposed as unrestricted new-key creation paths.

## Atomicity and implementation tradeoffs

SQLite uses one `INSERT ... SELECT` statement with both count gates and no overwrite on ID collision. Redis uses one Lua script that scans this application's literal escaped key prefix, reads only classification and owner fields, counts each record once, stops on a full quota, then inserts. Both derive counts from records, so deletion, restore, and process restart do not desynchronize counters. The API retries only random credential collisions, at most five attempts; database failures fail closed.

Redis SCAN inside Lua is atomic. Admission is bounded to 128 scan calls and 20,000 returned records (including non-ordinary keys), and fails closed with HTTP 503 if its inspection budget is exhausted. Existing key management remains usable. These are inspection budgets, not extra user quotas. It does not fetch existing friendly names or plaintext key records. A scan call uses COUNT 256 as a hint, not a Redis server execution-time guarantee. The bounded number of calls limits script amplification but individual Redis operation latency remains server-dependent. This patch is not a general Redis latency/rate-limit solution. Use an isolated application database and monitor latency at larger scale; an oversized shared keyspace can cause creation to return busy before the ordinary-key quota is reached. Persistent indexes/counters with backfill were deliberately avoided here to keep deletion/import compatibility and avoid a migration for a bounded endpoint.

## Verification

Regression tests cover raw/base64 and multibyte name boundaries, both create/rename, collisions, bounded retries, user/admin API capacity and recovery after deletion, and both provider implementations' concurrent admission, existing records, namespace isolation and global limits. Tests use synthetic identities and disposable databases, never production credentials. See the patch delivery notes for exact commands and observed results; unrun browser/production deployments are not implied.
