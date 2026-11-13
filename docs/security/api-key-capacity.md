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

## Local integration observations

The supplied patch was checked against master `3a9c5d4b9ef2b8470b47a412ab5960bd8a48ecab` and reviewed locally. The cloud API suite, targeted race checks, SQLite/Redis quota race checks, vet and noaws build results are reused for the unchanged implementation. Local API-suite and Redis-budget race runs completed successfully. A duplicate SQLite/Redis quota race run was stopped before completion, so it has no additional local pass result.

New local runtime verification used Redis 7.4.9 from the existing `redis:7-alpine` Docker image, with a disposable database and a host port bound only to `127.0.0.1`. Two independent provider instances shared the same literal namespace, including glob characters. Twenty concurrent contenders for a user's last slot and four for the global last slot each produced exactly one successful admission. One hundred update/delete races left the key deleted, and property updates retained an existing system key's TTL.

The actual Redis script also rejected admission without persisting the candidate when 20,001 system-key records exhausted the returned-record budget, and when 50,000 unrelated keys exhausted the SCAN-call budget. These checks exercised the native server's Lua and SCAN behavior.

Observed provider-call durations include local Docker/TCP overhead, queueing and Go race instrumentation.

| Scenario | Calls | Observed milliseconds |
| --- | --- | --- |
| User last slot, 20 concurrent contenders | 20 | 17.689–28.667 |
| Global last slot, 4 concurrent contenders | 4 | 378.919–379.046 |
| Returned-record budget exhaustion | 1 | 200.381 |
| SCAN-call budget exhaustion | 1 | 63.146 |

Production-load latency, Redis Cluster/replication and production deployments remain unverified. `COUNT` remains a hint, and the documented inspection budgets provide no strict elapsed-time guarantee. The temporary harness and databases are confined to local QA storage.
