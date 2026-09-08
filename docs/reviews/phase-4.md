# Phase 4 reconciliation review

Scope: read-only application-database reconciliation and its controlled
PostgreSQL acceptance scenario. Recovery remains the responsibility of the
existing durable delivery job; this review does not introduce automatic repair.

## Signals

`cmd/reconcile` opens the application database and emits one structured report.
The report counts pending or waiting payment events, payment conflicts, paid
orders without deliveries, impossible deliveries without payment, expired job
leases, and supplier operations whose result is unknown. It does not query
supplier databases and does not update application rows.

The separation is intentional: an observed inconsistency is evidence for an
operator, not authority to rewrite payment or delivery history. In particular,
an unknown supplier operation still must retry its persisted request ID and
must not authorize fallback.

## Acceptance evidence

`TestReconciliationReportsControlledAnomalies` creates, in an isolated
PostgreSQL test database, a waiting payment event, a conflict, a paid order
without delivery, an expired lease, and an unknown operation. It verifies the
exact report counts and then re-reads the source rows, proving that the report
does not hide anomalies through mutation. The CI integration job passed for
PR #8 (merge commit `9a6acd8`).

Recovery after supplier restock is covered by
`TestOutOfStockRestoresAfterRestock`: it adds a key only after final supplier
refusals, makes the durable job due, and verifies delivery through a new
operation. It is not an automatic reconciliation repair path.

## Accepted limits and revisit triggers

- The report is a point-in-time aggregate, not a historical audit ledger or a
  replacement for monitoring/alerting.
- It has no pagination or row drill-down; add bounded detail output before
  operating it against a large production dataset.
- Impossible rows can be created only by a broken migration, privileged manual
  SQL, or a database fault; the report detects them but cannot determine a safe
  correction.
- Supplier inventory and real payment-provider reconciliation require their
  own authoritative APIs and are outside the mock contract.
