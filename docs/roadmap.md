# Implementation and acceptance plan

Each stage ends with a verified change, focused diff, and concise report. Model/effort below is a project recommendation, not a comparative benchmark result. Before every work block, remind the user of one configuration; the user chooses model changes.

| Stage | Outcome | Done boundary | Model / effort |
| --- | --- | --- | --- |
| 0. Architecture | ADR, data model, verification matrix, separate repository | Timeout/event-order risks and guarantee boundaries are explicit | GPT-6 Astra / high |
| 1a. Foundation | Go module, health/readiness API, config, graceful shutdown, Makefile, Compose, base CI | Build, endpoint tests, `go vet`, and formatting verified; Docker was unavailable locally | GPT-5.6 Terra / medium |
| 1b. Schema and core flow | Migrations, seed data, orders, inbox, job, durable mock | Create → pay → deliver through PostgreSQL, invalid amount rejection, restock; integration and Compose CI green | GPT-6 Astra / high |
| 2. Races and crashes | Process tests: 50 webhooks, SIGKILL, fencing | PostgreSQL integration plus three `make reliability` repetitions in CI | GPT-6 Astra / high |
| 3. Two suppliers | Controlled/random failures, backoff, unknown outcome, safe A → B | Timeout after issue does not consume a second code; fallback follows only a durable refusal | GPT-6 Astra / high |
| 4. Reconciliation and recovery | Structured logs, reconciliation, recovery after restock | Read-only report finds controlled anomalies; queue recovers after restock | GPT-6 Astra / high |
| 5. Catalog | Thousands of SKUs, availability query, indexes | Reproducible fixture and `EXPLAIN (ANALYZE, BUFFERS)` input; actual measurements remain environment-specific | GPT-6 Astra / high |
| 6. Handoff | README, reproduction scenarios, scaling note, actual time | Clean-clone reproduction, race/integration/security checks, final review | GPT-6 Astra / high |

Stages 1–2 are mandatory in the assignment. Stage 3 is strongly desired. Stages 4–5 are bonus work after the reliable core. A balanced money ledger is an additional stage-4 bonus and is not claimed here.

## Acceptance matrix

The process suite in [PR #2](https://github.com/krav01/digital-goods-core/pull/2) implements the concurrency/crash scenarios; the README documents the current reproducible commands. Confirm results against CI for a concrete commit. Supplier B, reconciliation, catalog fixtures, and final delivery documentation were completed in later merged PRs.

| ID | Scenario | Expected result | Stage |
| --- | --- | --- | --- |
| A01 | Create order, pay, wait for worker | One code, delivered state, matching price/currency | 1b |
| A02 | 50 parallel paid events with one event ID | One event row, one delivery, one consumed key | 2 |
| A03 | 50 distinct paid event IDs for one order | Events retained; one delivery and job | 2 |
| A04 | Replay after delivery | Code, state, payment, and delivery unchanged | 2 |
| A05 | Webhook before order, then create the known ID | Event retained; delivery finishes after order creation | 1b/2 |
| A06 | paid then failed; failed then paid | No final-state rollback; conflict is visible in reconciliation | 1b/4 |
| A07 | Invalid amount/currency or changed same-ID payload | No delivery; event rejected or ID conflicts | 1b |
| A08 | Supplier issued a code but response was lost | Same request ID returns the old code; B is not called | 3 |
| A09 | A persists final unavailable, B succeeds | B issues once; A still refuses late replay | 3 |
| A10 | A hangs or returns ordinary 5xx | Unknown result persists; B is not used | 3 |
| A11 | Both suppliers empty, then restocked | `out_of_stock` recovers; new cycle follows final refusals only | 3/4 |
| A12 | Two orders contend for the final key | One owns the code; the other remains recoverable | 2 |
| A13 | Multiple workers claim one job | Requests may repeat; effects may not | 2 |
| A14 | Crash after webhook/payment commit before job processing | Inbox/job survive and delivery finishes after restart | 2 |
| A15 | Crash after supplier commit before application commit | Retry restores the same code | 2/3 |
| A16 | Lease expires; another worker proceeds; old reply arrives late | Old fence cannot change state; one code | 2 |
| A17 | A refuses, B selection persists, old A worker retries | A replays refusal; B delivers once | 3 |
| A18 | Concurrent same-ID issue; then a mismatched payload | One result; mismatched payload rejected | 2/3 |
| A19 | Delivery without payment / paid without delivery | Reconciliation exposes, rather than rewrites, the anomaly | 4 |
| A20 | Storefront with thousands of SKUs | Query plan and actual latency/data volume recorded | 5 |

Supplier HTTP-call count may exceed one. Count durable successful operations and consumed keys in **both supplier databases**, not just application `deliveries` rows. Race tests use PostgreSQL and independent processes, not only mocks and `go test -race`.

## Verification order

Run formatting/build first, then targeted tests and vet/lint. After a coherent high-risk change, run relevant integration/race tests and validate database invariants. Run security and load checks at their relevant stages. Never publish invented green CI or commands that were not run. The final README contains exact local commands for races, timeout-after-issue, fallback, recovery, environment limits, and accepted trade-offs.
