# Digital Goods Core

Go-бэкенд магазина цифровых товаров: заказы, платёжные вебхуки и однократная выдача товара при повторах, гонках и сбоях поставщиков.

**Статус:** этап 1b — реализованы заказы, долговечный inbox платежей, очередь выдачи, миграции PostgreSQL и поставщик A с отдельной БД. Полный запуск Compose и интеграционные проверки PostgreSQL [прошли в CI](https://github.com/krav01/digital-goods-core/actions/runs/34162188953) для `0e77e26`. Поставщик B и испытания гонок независимых процессов — следующие этапы.

## Принятые решения

- Модульный монолит на Go, зависимости через обычные конструкторы; без DI-фреймворка.
- PostgreSQL — источник истины для заказов, входящих событий и надёжных фоновых задач.
- `net/http`, `pgx/v5`, `log/slog`, `golang-migrate`; версии закреплены в `go.mod` и `go.sum`.
- API и worker запускаются отдельными процессами из одного модуля. Заглушка A доступна по HTTP с независимой БД; B запланирована.
- Доставка сообщений и выполнение задач допускают повторы. Однократность бизнес-эффекта обеспечивается транзакциями и идемпотентностью поставщика.
- После неопределённого результата A переключение на B запрещено. Исчерпание повторов не доказывает отсутствие выдачи.

## Документация

- [Архитектура и границы гарантий](docs/architecture.md)
- [Модель данных и API](docs/data-model.md)
- [Этапы и матрица приёмки](docs/roadmap.md)
- [Правила работы](AGENTS.md)

## Quick start

Requires Docker with Compose v2. The demo uses PostgreSQL 18.6, applies migrations, and starts API, worker and supplier A. Published ports are loopback-only; the credentials in Compose are disposable development credentials, not production secrets.

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
SUPPLIER_URL=http://127.0.0.1:8081 DATABASE_URL='postgres://app:app-dev-only@127.0.0.1:5432/goods?sslmode=disable' ./bin/worker
```

Configuration:

| Переменная | По умолчанию | Назначение |
| --- | --- | --- |
| `HTTP_ADDR` | `:8080` | Адрес HTTP-сервера |
| `SHUTDOWN_TIMEOUT` | `5s` | Предельное время корректного завершения |
| `DATABASE_URL` | required | Application or supplier PostgreSQL DSN, depending on the process |
| `SUPPLIER_URL` | `http://127.0.0.1:8081` | Worker-to-supplier HTTP endpoint |

## Checks

```sh
make check
make race
golangci-lint run ./... # v2.13.2
go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...
# Dedicated disposable PostgreSQL server; role needs CREATEDB:
TEST_DATABASE_URL='postgres://test:test@127.0.0.1:5432/postgres?sslmode=disable' make integration
```

`make check` runs formatting, compilation, shuffled unit tests and vet. Integration tests create and drop only uniquely named `dgc_*_test` databases; do not point them at a production server. They cover the HTTP flow, early/replayed/invalid payments, terminal-state conflicts, persisted supplier replay/refusal, modeled worker loss after supplier commit, stale-lease rejection, and restock recovery. These are not yet the multi-process race/fault suite planned for phases 2–3.

CI also builds and starts Compose from a clean checkout and runs the smoke script. Local Docker execution was unavailable in the development sandbox; consult the [CI runs](https://github.com/krav01/digital-goods-core/actions/workflows/ci.yml) for PostgreSQL/container verification.

## Reliability boundaries

Payment application and job creation share a transaction. Workers claim jobs with `SKIP LOCKED`, a 15-second lease and fencing version. Supplier requests are persisted before HTTP I/O; HTTP calls never hold application transactions open. An ambiguous result retries the same request ID against A, with persisted exponential backoff from 1 to 32 seconds. A durable refusal permits a fresh attempt after restock. Both `out_of_stock` and `delivery_failed` are recoverable.

The mock supplier saves issuance and stock consumption atomically and replays both success and final refusal for a request ID across restarts. A timeout does not authorize fallback. This contract must be supported by any real supplier before claiming equivalent guarantees. Inventory admin tools, reconciliation, authentication, rate limiting, production secrets and multi-supplier fault controls are not implemented.

Исходное [тестовое задание](https://docs.google.com/document/d/11ouRyL-sjW5110I6iRgPG_7WkPaJvUjTZjAE5Ta_OOo/edit) требует backend без фронтенда, настоящего эквайринга и реальных поставщиков. Подпись вебхука по условиям не проверяется; это учебное упрощение, а не готовая схема публичного production-сервиса.

## Учёт времени

Начало работ над репозиторием: 2026-09-07. Фактическое время будет записываться по завершённым рабочим блокам; оценки и паузы между сессиями не выдаются за время разработки.
