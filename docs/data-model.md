# Data model and API

SQL migrations implement this model. Monetary amounts are stored as `BIGINT` kopecks and currency is `RUB`. The external `amount` field preserves the source contract's ruble unit: a decimal with at most two fractional digits is parsed without `float64`, range-checked, and converted to kopecks. The server snapshots the catalog price onto the order.

## Application database

| Table | Key fields / constraints | Purpose |
| --- | --- | --- |
| `products` | `sku PK`, name, type, price_minor > 0, currency, active | Catalog seeded with the assignment's 12 SKUs |
| `orders` | `id PK`, sku FK, price_minor, currency, status, paid_at, timestamps | Price snapshot and lifecycle |
| `payment_events` | `event_id PK`, order_id without FK, status, amount_minor, currency, created_at, received_at, processing_state, next_attempt_at, error | Inbox; no FK permits an event before its order |
| `delivery_jobs` | `order_id PK/FK`, state, available_at, leased_until, lease_version, attempts, last_error | One reliable job per order |
| `delivery_operations` | `request_id PK`, order_id FK, supplier, generation, sku, state, reason; unique(order_id, supplier, generation) | Stable external-operation identity and supplier-selection history |
| `deliveries` | `order_id PK/FK`, request_id unique FK, supplier, code unique, delivered_at | One completed result per order; a code cannot reach two orders |

Indexes include a partial index on due inbox events by `next_attempt_at`, an inbox `order_id` index for early/conflicting events, job indexes for readiness and expired leases, and an operation `order_id` index. Their exact form and use are verified against real SQL queries.

The “delivered only after payment” invariant comes from checking the locked order and committing completion in one transaction; an FK alone cannot provide it. The `delivered` state and `deliveries` row change together. `UNIQUE(code)` is an additional local guard, not proof that an external supplier cannot issue twice.

## Each supplier database

| Table | Constraints | Purpose |
| --- | --- | --- |
| `inventory_keys` | key_id PK, code unique, sku, issued_request_id unique nullable | Test keys by SKU; atomic allocation of an available key |
| `issue_requests` | request_id PK, order_id, sku, outcome, code, reason; unique(order_id) where outcome = issued | Immutable operation result: issued or finally refused |

One key can be assigned to only one successful operation. A/B receive non-overlapping portions of the original 50-key pool; SKU allocation is fixed in seed data. Generated load-test keys are separately marked and non-overlapping as well.

## Order statuses

- `created → paid → delivering → delivered` — primary path.
- `created → payment_failed` — final failed payment.
- `delivering → out_of_stock` — confirmed stock absence without an unfinished unknown operation.
- `delivering → delivery_failed` — delivery deferred after refusals or an unknown result; the exact reason remains on the job/operation.
- `out_of_stock / delivery_failed → delivering` — safe recovery accounting for earlier operations.

Final `delivered` and `payment_failed` states never change automatically. Table tests cover each transition pair, including prohibited transitions and repeats. Contradictory payment events are retained for reconciliation as defined by ADR-001.

## HTTP endpoints

| Method and path | Input / response |
| --- | --- |
| `POST /orders` | `{sku, order_id?}` → `201` with price snapshot and state. Reusing a client ID with the same SKU returns the existing order (`200`); another SKU returns `409` |
| `GET /orders/{id}` | `200` with state; code only for `delivered`; unknown ID is `404` |
| `POST /webhooks/payment` | Assignment contract (`event_id`, `order_id`, `status`, `amount`, `currency`, `created_at`); `200` after durable acceptance/exact replay; conflicting ID is `409`; malformed input is `400`; persistence failure is `5xx` |
| `GET /healthz` | Process health check |
| `GET /readyz` | Database availability and applied-migration check |
| `POST /issue` at A/B | Assignment contract; the terminal-error extension with `request_id` and `final` is defined in ADR-001 |

A payment event accepted over HTTP can later be rejected by amount validation. HTTP `200` means “the event was durably stored”, not “the item was delivered”. `created_at` is validated as a timestamp but is not used as the sole proof of authoritative ordering.

Test-only endpoints or CLIs for fault modes and restocking must not be exposed as normal public API. Reconciliation and catalog functionality are independent stages; public-production authentication is outside the assignment scope.
