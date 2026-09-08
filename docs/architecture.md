# ADR-001: Reliable delivery of digital goods

Status: accepted and implemented. Go and a modular monolith with manual DI were agreed with the project owner.

## Context and goal

Webhooks may be repeated and arrive out of order. Workers may run concurrently and stop at any time. A supplier may issue a code and lose its response. The goal is one delivery effect per paid order, without consuming a key twice.

Safety depends on PostgreSQL durability and mock-supplier contract compliance. Completion also requires a healthy database and supplier, an active worker, and available stock. When A is permanently unavailable after an unknown result, it is impossible to guarantee both completion and no duplicate issuance at independent B.

## Components and boundaries

The single Go module is `github.com/krav01/digital-goods-core`:

- `cmd/api` validates HTTP requests and accepts payment events.
- `cmd/worker` processes the inbox and performs retried, recoverable delivery.
- `cmd/supplier` runs one mock; two instances with separate storage represent A/B.
- `cmd/migrate` applies scoped embedded migrations; `cmd/reconcile` emits a read-only report.

HTTP/SQL adapters depend on application types; application logic does not import transport packages. Narrow interfaces live with their consumer. Generic repositories and a DI container are not needed. The application has one database; each supplier has another database reached only over HTTP. Tests may inspect all databases, business logic may not. A/B key pools never overlap.

## Payment intake: durable inbox

1. Validate the event shape, permitted status, and exact monetary representation.
2. Persist the event with a unique `event_id` in a short transaction, then return `200 OK`.
3. The same ID with the same meaningful fields is a safe replay; changed fields produce `409 Conflict` without altering the original event.
4. Return `5xx` when persistence is unavailable so the sender retries.
5. The inbox worker locks the existing order, validates price/currency and transition, updates the order, creates its single delivery job, and marks the event processed in one transaction.

Before an order exists, an event stays `waiting_order`: it is not lost and cannot create an order without SKU/price. The API accepts a client-provided `order_id` for reproducible early-webhook tests. The assignment has no payment-attempt ID or authoritative state version, so `created_at` cannot define ordering. Consequently, `paid` after payment is a replay, `failed` after payment/delivery does not roll it back, `paid` after `payment_failed` is a reconciliation conflict, and an amount/currency mismatch is rejected without delivery.

## Delivery job in PostgreSQL

`delivery_jobs` is both a transactional outbox and work queue. A worker claims a ready job through `FOR UPDATE SKIP LOCKED`, sets a lease and increases `lease_version`, then commits before HTTP I/O. It creates or loads a durable supplier operation under short locks and calls the supplier outside the transaction. A completion transaction applies the result only when the lease version still matches.

An expired lease permits another worker to continue. Fencing prevents an old worker from changing local state, while the persisted `request_id` and durable supplier idempotency protect the external effect. Completion atomically writes `delivery`, marks the order `delivered`, and closes the job. A crash after supplier issuance but before the application commit repeats the same operation rather than issuing another code.

Transactions are short and lock in a consistent inbox event → order → delivery job order. Deadlock/serialization errors may retry the local transaction; network calls do not join that retry.

## Mock contract and safe fallback

The base supplier contract is `POST /issue` with `request_id`, `order_id`, and `sku`; a successful replay must return the same code. Safe fallback needs durable final refusals as well as durable successes. The mocks bind each ID to its original order/SKU and atomically reserve one key or persist an immutable refusal. A mismatched replay conflicts; concurrent matching requests read the same result.

| Application observation | Action |
| --- | --- |
| Successful response with matching `request_id` and code | Atomically complete delivery |
| Timeout, connection loss, malformed response, ordinary `5xx` | Result is unknown; retry the same ID at the same supplier |
| Confirmed durable final refusal for that ID | Persist choice of B and create B's operation ID |
| Fast retries exhausted while outcome is unknown | Defer another check with the same ID; do not fall back |

For a confirmed refusal, the mock returns `status: error`, `reason`, `request_id`, and `final: true`. This is a documented extension to the assignment. `503` alone is never proof of refusal. A/B calls for one order are never parallel. The A → B choice is persisted before B is called. A new cycle after restock is allowed only after every earlier operation finally refused; unknown operations retain their original ID.

Backoff uses jitter and a cap. HTTP calls have deadlines and observe `context.Context`. Random failure modes are configurable, while tests use deterministic controls and barriers rather than depending on chance.

## Recovery and observability

Reconciliation reports pending-order events, payment conflicts, paid orders without delivery, delivery without payment, expired leases, and unknown supplier operations. `out_of_stock` and `delivery_failed` are recoverable, but an order state never permits fallback after an unknown supplier result.

Structured logs include `order_id`, `event_id`, `request_id`, supplier, attempt, outcome, and duration. Codes and secrets are not logged. After a forced stop, the durable inbox, job, and supplier operation remain the recovery source.

## Alternatives and revisit triggers

- Redis/RabbitMQ are deferred to avoid a second atomicity boundary; revisit after a measured PostgreSQL-queue bottleneck.
- `sync.Mutex` cannot provide exactly-once behavior across processes or restarts.
- A transaction around HTTP holds locks and cannot undo an external effect.
- Fallback after N timeouts may consume two items and is rejected.
- Infinite rapid retries create load and are replaced by deferred checks.

If a real supplier lacks durable idempotency and a final-refusal/status/cancellation contract, these guarantees cannot be transferred unchanged; require a new contract or explicitly weaker semantics.

## Verified primary sources

- [PostgreSQL: row locks](https://www.postgresql.org/docs/current/explicit-locking.html): row locks last until transaction completion.
- [PostgreSQL: SELECT / SKIP LOCKED](https://www.postgresql.org/docs/current/sql-select.html): skipping locked rows is appropriate for multiple queue consumers, not consistent storefront reads.
- [Go module layout](https://go.dev/doc/modules/layout).
- [pgxpool](https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool).
