# Digital Goods Core

Go backend for a digital-goods store: orders, payment webhooks, and one-time delivery across retries, races, and supplier failures.

**Status:** all assignment phases are implemented: a durable payment inbox, a PostgreSQL delivery queue, independent A/B suppliers, exactly-once behavior under process races, reconciliation, and catalog support. The complete `check`, `compose`, and `integration` suite passed in [CI PR #15](https://github.com/krav01/digital-goods-core/pull/15).

## Key decisions

- Go modular monolith with ordinary constructor-based dependencies and no DI framework.
- PostgreSQL is the source of truth for orders, inbound events, and reliable background jobs.
- `net/http`, `pgx/v5`, `log/slog`, and `golang-migrate`; versions are pinned in `go.mod` and `go.sum`.
- The API and worker run as separate processes from one module. A/B mocks are exposed over HTTP with independent databases.
- Message delivery and job execution may be retried. Transactions and supplier idempotency guarantee the single business effect.
- An unknown result from A never permits fallback to B. Exhausted retries do not prove that delivery did not occur.

## Documentation

- [Architecture and guarantee boundaries](docs/architecture.md)
- [Data model and API](docs/data-model.md)
- [Implementation and acceptance matrix](docs/roadmap.md)
- [Working rules](AGENTS.md)

## Quick start

Requires Docker with Compose v2. The demo uses PostgreSQL 18.6, applies migrations, and starts API, worker, and independent suppliers A/B. Published ports are loopback-only; the credentials in Compose are disposable development credentials, not production secrets.

```sh
docker compose up -d --build
curl --fail http://127.0.0.1:8080/readyz
./scripts/smoke.sh
```

The smoke script requires `curl`, `jq`, and Bash. It creates a Steam 500 RUB order, submits the same payment twice and waits for a nonempty code. Stock is finite: A has 25 of the assignment's 50 sample keys, including three Steam 500 keys; the other 25 are reserved for B. Repeated smoke runs consume stock. `docker compose down` stops the demo and preserves data in named volumes.

```sh
curl --fail -H 'Content-Type: application/json' \
  -d '{"order_id":"ord_demo_1","sku":"STEAM-TOPUP-500"}' \
  http://127.0.0.1:8080/orders

curl --fail -H 'Content-Type: application/json' \
  -d '{"event_id":"evt_demo_1","order_id":"ord_demo_1","status":"paid","amount":500,"currency":"RUB","created_at":"2026-09-07T12:00:00Z"}' \
  http://127.0.0.1:8080/webhooks/payment

curl --fail http://127.0.0.1:8080/orders/ord_demo_1
docker compose logs worker
```

`POST /orders` returns 201 for a new order, 200 for the same ID/SKU, and 409 for an ID reused with another SKU. Omit `order_id` to generate one. `GET /orders/{id}` returns `price_minor` in kopecks and exposes `code` only after delivery. Payment `amount` is in RUB: a positive decimal with at most two fractional digits, without exponent notation. No floating-point arithmetic is used.

Webhook HTTP 200 means the event was durably accepted, **not** that payment validation or delivery succeeded. Exact semantic replays are accepted; changed payloads with the same event ID return 409. The worker records amount/currency mismatches as `rejected`. Events received before order creation wait durably. `failed` after payment and `paid` after `payment_failed` become `conflict`, without undoing a final state.

`/healthz` checks the process; `/readyz` checks the database connection and clean migration version. Application and supplier DSNs must point to their respective databases. Migration scopes are not interchangeable.

## Native Go commands

Requires Go 1.26.8+ and the two databases. Compose already runs migrations; for an independently provisioned database, run `DATABASE_URL=... go run ./cmd/migrate -scope app` or `-scope supplier` once before starting the corresponding process. Migrations are embedded, versioned and up-only; dirty migrations require operator diagnosis, not an automatic force/reset.

```sh
make build
# Run each in a separate terminal when the corresponding Compose process is stopped:
DATABASE_URL='postgres://app:app-dev-only@127.0.0.1:5432/goods?sslmode=disable' ./bin/api
HTTP_ADDR=127.0.0.1:8081 DATABASE_URL='postgres://supplier:supplier-dev-only@127.0.0.1:5433/supplier_a?sslmode=disable' ./bin/supplier
DATABASE_URL='postgres://supplier:supplier-dev-only@127.0.0.1:5434/supplier_b?sslmode=disable' ./bin/migrate -scope supplier-b
HTTP_ADDR=127.0.0.1:8082 DATABASE_URL='postgres://supplier:supplier-dev-only@127.0.0.1:5434/supplier_b?sslmode=disable' ./bin/supplier
SUPPLIER_URL=http://127.0.0.1:8081 SUPPLIER_B_URL=http://127.0.0.1:8082 DATABASE_URL='postgres://app:app-dev-only@127.0.0.1:5432/goods?sslmode=disable' ./bin/worker
DATABASE_URL='postgres://app:app-dev-only@127.0.0.1:5432/goods?sslmode=disable' ./bin/reconcile
```

`reconcile` is read-only. It writes one structured report with counts of pending/conflicting payment events, paid orders without delivery, impossible delivery-without-payment rows, expired delivery leases, and unknown supplier operations; it never repairs data automatically.

For the phase-5 catalog fixture, use only a disposable application database:

```sh
psql "$DATABASE_URL" -f scripts/catalog-fixture.sql
psql "$DATABASE_URL" -f scripts/catalog-explain.sql
```

The fixture inserts exactly 10,000 deterministic active `FIXTURE-SKU-*` rows without changing the base catalog. Save the emitted plan with the PostgreSQL version and database settings; it is an observation, not a portable latency claim.

Configuration:

| Variable | Default | Purpose |
| --- | --- | --- |
| `HTTP_ADDR` | `:8080` | HTTP server address |
| `SHUTDOWN_TIMEOUT` | `5s` | Maximum graceful-shutdown duration |
| `DATABASE_URL` | required | Application or supplier PostgreSQL DSN, depending on the process |
| `SUPPLIER_URL` | `http://127.0.0.1:8081` | Worker-to-supplier HTTP endpoint |
| `SUPPLIER_B_URL` | `http://127.0.0.1:8082` | Worker-to-supplier B HTTP endpoint |
| `SUPPLIER_AFTER_ISSUE_DELAY` | unset | Delay response after durable issue; `3s` reproduces timeout-after-issue with the worker's 2s client timeout |
| `SUPPLIER_FORCE_FINAL_UNAVAILABLE` | `false` | Always persist a final refusal instead of issuing a key |
| `SUPPLIER_FINAL_UNAVAILABLE_PERCENT` | `0` | Percent (0–100) of durable final refusals before issue |
| `SUPPLIER_TRANSIENT_ERROR_PERCENT` | `0` | Percent (0–100) of non-final 503 responses before supplier storage is called |

## Checks

```sh
make check
make race
golangci-lint run ./... # v2.13.2
go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...
# Dedicated disposable PostgreSQL server; role needs CREATEDB:
TEST_DATABASE_URL='postgres://test:test@127.0.0.1:5432/postgres?sslmode=disable' make integration
TEST_DATABASE_URL='postgres://test:test@127.0.0.1:5432/postgres?sslmode=disable' make reliability
TEST_DATABASE_URL='postgres://test:test@127.0.0.1:5432/postgres?sslmode=disable' ./scripts/reliability.sh
```

`make check` runs formatting, compilation, shuffled unit tests and vet. Integration tests create and drop only uniquely named `dgc_*_test` databases; do not point them at a production server. They cover the HTTP flow, early/replayed/invalid payments, terminal-state conflicts, persisted supplier replay/refusal, stale-lease rejection, and restock recovery.

`make reliability` runs the phase 2 multi-process scenarios three times with race detection and shuffled order: 50 concurrent webhooks (same/distinct IDs and before order creation), four workers, concurrent supplier requests, last-key contention, SIGKILL before/after transaction boundaries and late-result fencing. Tests print process IDs and scenario outcomes; successful effects are checked in both application and supplier A databases. One crash test waits for the real 15-second lease expiry. Test-only checkpoints pause the production worker through an adapter; they are not production configuration. Requires a Unix-like OS for SIGTERM/SIGKILL; the verified CI platform is recorded with the phase result. See [phase 2 review and limits](docs/reviews/phase-2.md).

`scripts/reliability.sh` is the acceptance entry point for the same real HTTP webhook harness: it delegates to `make reliability` and requires `TEST_DATABASE_URL`. It sends the 50 parallel payment webhooks itself; no external payment provider is involved.

To reproduce the final A → B fallback with Compose, reset the disposable volumes, start A in its durable-final-refusal mode, then run the normal payment stub:

```sh
docker compose down -v
SUPPLIER_A_FORCE_FINAL_UNAVAILABLE=true docker compose up -d --build
./scripts/smoke.sh
docker compose logs worker
```

The resulting order is delivered from B. To exercise timeout-after-issue instead, start with `SUPPLIER_A_AFTER_ISSUE_DELAY=3s`; the worker retries A with the same persisted `request_id` and does not use B. Prefix the other fault settings with `SUPPLIER_A_` or `SUPPLIER_B_` in Compose (for example, `SUPPLIER_B_TRANSIENT_ERROR_PERCENT=25`).

CI also builds and starts Compose from a clean checkout and runs the smoke script. Local Docker execution was unavailable in the development sandbox; consult the [CI runs](https://github.com/krav01/digital-goods-core/actions/workflows/ci.yml) for PostgreSQL/container verification.

## Reliability boundaries

Payment application and job creation share a transaction. Workers claim jobs with `SKIP LOCKED`, a 15-second lease and fencing version. Supplier requests are persisted before HTTP I/O; HTTP calls never hold application transactions open. An ambiguous result retries the same request ID against A, with persisted exponential backoff from 1 to 32 seconds. A durable refusal permits a fresh attempt after restock. Both `out_of_stock` and `delivery_failed` are recoverable.

Each mock supplier has independent durable storage and an independent key pool. The mock saves issuance and stock consumption atomically and replays both success and final refusal for a request ID across restarts. A durable refusal from A can select B; a timeout does not authorize fallback. For deterministic fault scenarios use `SUPPLIER_AFTER_ISSUE_DELAY` or `SUPPLIER_FORCE_FINAL_UNAVAILABLE`; `SUPPLIER_FINAL_UNAVAILABLE_PERCENT` (0–100) samples durable final refusals before issue. `SUPPLIER_TRANSIENT_ERROR_PERCENT` (0–100) returns a non-final 503 before a supplier operation; the worker retries the same request ID against A and must not fall back to B. Set either variable independently for supplier A or B. This contract must be supported by any real supplier before claiming equivalent guarantees. Inventory admin tools, automatic reconciliation repair, authentication, rate limiting and production secrets are not implemented.

The original [assignment](https://docs.google.com/document/d/11ouRyL-sjW5110I6iRgPG_7WkPaJvUjTZjAE5Ta_OOo/edit) requires a backend only, with no live payment acquirer or real suppliers. Webhook signature verification is explicitly out of scope; this is an educational simplification, not a production public-service design.

## Time tracking

Repository work started on 2026-09-07. Actual time is recorded only for completed work blocks; estimates and gaps between sessions are not presented as development time.
