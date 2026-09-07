# Phase 2 reliability review

Scope: multi-process integration tests and their CI gate. The production
transaction/HTTP protocol is unchanged. This is a separate manual review pass,
not an independent-agent review.

## Test architecture

`process_test.go` starts the race-instrumented test executable as independent
API, supplier and worker processes, each with its own PostgreSQL pool. They use
the production handlers, stores, supplier client and Worker.Run. The test
adapter only pauses after committed operations or immediately before applying
the supplier result; it does not replace transaction logic. This verifies the
protocol across processes, not the exact cmd/ startup code (covered by Compose).

Parent/child gates use pipes; request bursts have a common start barrier. The
test waits for durable SQL predicates, explicit checkpoint messages and actual
OS process exit. SIGKILL status is checked rather than treating any exit as a
simulated crash. Each child has a bounded timeout and owned cleanup; loss of the
parent's stdin also cancels the child. No crash controls ship in production.

## Acceptance scenarios

| Scenario | Evidence asserted |
| --- | --- |
| 50 same-ID webhooks + repeat after delivery | One inbox row, unchanged paid_at/code, one job/operation/delivery and consumed supplier key |
| 50 distinct event IDs + repeat | All 50 events applied; four worker processes attempt claims; same single-effect invariants |
| 50 events before order creation | All events wait durably, then finish after order creation |
| Concurrent supplier calls | 50 identical replies; changed order/SKU or new ID for the issued order cannot consume another key |
| Two orders, last key | One delivery and one recoverable out_of_stock; restock completes the other order with a distinct key |
| API killed after HTTP 200 | Pending inbox survives; restarted API/worker complete delivery |
| Worker killed while blocked before payment commit | Original unpaid order and pending event remain; no job leaks from the rolled-back transaction |
| Worker killed after payment/job commit | Replacement finds the durable job |
| Worker killed after operation preparation | Replacement waits for the real 15-second lease and reuses the operation |
| Worker and supplier killed after supplier commit | Supplier restart preserves issuance; replacement worker records the same request/code |
| Old worker result after lease takeover | Actual old process returns ErrLeaseLost; one external effect and lease_version=2 |

For the late-result and supplier-restart cases the test expires the lease by
updating only its disposable test database. The preparation-crash case tests
natural expiry without this shortcut. Application request/code values are joined
against supplier issuance and inventory records, not only counted locally.

## Limits and gate

- Supplier B and cross-supplier effect counts remain phase 3; B does not exist yet.
- This is not a database-server/power-loss test. PostgreSQL durability is assumed;
  application and supplier processes, not PostgreSQL servers, are killed.
- A supplier response lost through a transport fault and controlled A/B refusal
  schedules remain phase 3. The current barrier is after the supplier response
  but before the application result transaction.
- This suite is a bounded regression test, not an exhaustive proof of all
  possible schedules or a throughput benchmark.
- `make reliability` repeats the phase 2 suite three times with shuffled test
  order and race instrumentation. PostgreSQL execution must pass in CI before
  declaring this phase verified; local compilation/lint cannot substitute for it.
