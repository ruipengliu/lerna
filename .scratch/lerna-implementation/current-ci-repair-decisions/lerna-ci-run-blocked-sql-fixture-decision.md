# Blocked-SQL Run fixture: faithful connection setup — STATIC

**Adopt parseable DSNs with exact application-name provenance and an explicit finite three-connection preparation before Run. Reject the extra LockPool preflight/delegation proposal.** No product connector port, Core/PoolScope change, token substitution or driver-error injection is needed.

## Why

The actual first normal attempt proved three original pool advisory waiters, then failed after release with `PostgreSQL scope configuration rejected`. It did not reach its normal public tail. Test source SHA `1027c6a1ce151fe34e3cfea549ba4d1629d69e1e0c8efe4f8a668c4e91a1cec6`; 12-line failure SHA `a33f88219aa254bc530cde42aafa31b89072d220b6920eaba4ffc0b5fef3d827`. Preserve this fixture-configuration failure, including its native exit1 and actual absence; it is not a new cancellation business red.

`Core.Open` uses fixed `sql.Open("pgx", cfg.DSN)`. `PoolScope` reads the durable scope and independently parses that same DSN to bind host/port/database/schema. A stdlib registry handle is not that parseable configuration. Replacing the real Run transaction with a preflight lock on another Store would prove a different path.

## Minimal assembly

1. Before opening either peer, durably register the owned schema and unique role/application-name intent. Use a short ASCII tag derived from the already-owned scope plus role, below PostgreSQL’s truncation limit. Keep a parseable original DSN, changing only `application_name` through its proper URI-query or keyword-DSN encoding. Do not print credentials. Check the parsed effective tag and unchanged scope connection identity. Remove opaque registration and its unregister cleanup.
2. Open the original actual Store; retain its partial-handle and first-Close rules. Use the same Store for Runner, Repository, Clock, worker and business callbacks. Keep the observer separate. Existing observer/backend registration records should include `backend_start` with PID to avoid PID-reuse ambiguity.
3. Before the holder/Run experiment, explicitly prepare three simultaneous connections using three bounded **ordinary Store.Within transactions**, with a finite barrier keeping all three active until the observer identifies them. No LockPool, business mutation or fake error in these preparation callbacks. Core’s normal transaction startup SQL is sufficient; these are acknowledged fixture preparation transactions, not the tested blockage. Register the exact tag/database/user/PID/backend-start observations, release all callbacks and join all three actual transaction results. Confirm their idle identities before proceeding. Preserve default MaxOpen16/MaxIdle4; do not pin or replace connections through a new product API.
4. Register the actual blocker identity likewise before starting Run. Then use the existing real holder and unchanged Run callbacks. Positively observe all three registered Run identities waiting on the exact original `PoolCoordination` advisory SQL and exact blocker. A replacement/unregistered backend, failed registration, timeout or uncertain setup transaction is preparation failure: STOP, retain facts and scope uncertainty, no retry or borrowed identity.

Scope ownership exists before connection creation; actual PID registration necessarily follows backend creation and can follow handshake, Ping and setup SQL. This is **after-startup/pre-Run** qualification, never “before backend exists” or “before first SQL.” Preparation consumes the original caller budget and is not a performance optimization. Future pool connections remain owned by the same Store/scope; this initial three-PID observation does not claim an exhaustive lifetime connection census.

## Qualification and limits

Retain original Tx3s/statement2s/lock1s/caller15s, default16, lease1m, fallback10ms and aggregate120s. No renewed caller or hidden setup allowance. First execute the independent normal release counterpart; require all original projections, reservations, later ordinary completion, fixed receipts and every entry joined. Only after success run caller cancellation at the positively observed original SQL waits, preserving all actual errors without demanding a particular native driver error. Failed first Close remains UNKNOWN regardless of later absence. No previous failure is overwritten, and no native execution or product edit occurred in this decision.
