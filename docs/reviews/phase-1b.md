# Phase 1b architecture review

Scope: order creation, durable payment inbox, delivery job/lease, supplier A,
schema migrations, process wiring and the reproducible demo. This is a separate
manual review pass against ADR-001, not an independent-agent review.

## Findings addressed

- Explicitly type interval parameters as bigint so pgx encodes millisecond
  durations consistently for lease and retry SQL.
- Keep Go caches outside the repository: placing a module cache inside `work/`
  made module discovery traverse cached dependency directories.
- Upgrade the build toolchain to Go 1.26.8 and `x/text` to v0.39.0 after
  govulncheck reported reachable vulnerabilities in the initial versions.
- Handle response/connection close errors; worker cancellation is a normal exit.

## Invariants checked in code

- Payment update, event result and job insertion share one transaction.
- Event IDs cannot replace existing payment payloads. Order locking serializes
  payment decisions without trusting event timestamps as an authoritative order.
- Job claiming commits before order locking or external I/O. Prepare and finish
  lock order then job; finishing requires the current, unexpired lease version.
- Request IDs are persisted before supplier calls. Unknown outcomes reuse the
  same ID. A new generation requires a durable refusal from the previous one.
- Supplier stock consumption and issuance commit together in an independent
  database. Successful issuance has unique request ID, order ID and code.
- Supplier refusals remain final after restock. External calls are not repeated
  inside database transaction retries; ambiguous database commits are not blindly
  retried by the transaction helper.

## Accepted limits and revisit triggers

- One supplier and one external call per worker step. Introduce B and jitter in
  phase 3; never use timeout/retry exhaustion as evidence for safe fallback.
- Restock retries can accumulate refused operation rows. Define retention and
  backoff policy before sustained production workloads; do not remove unresolved
  or idempotency history without a new protocol decision.
- `payment_failed` is final; a later paid event requires manual reconciliation.
  Revisit only with an authoritative payment-attempt/version contract.
- Separate DSNs and migration scopes are deployment prerequisites; readiness
  checks the migration version, not the identity of every required table.
- Multi-process races, actual process-kill recovery and both-supplier effect
  counts remain phase 2/3 acceptance work. Current recovery tests model the
  relevant commit boundary and expired lease within a test process.
- Compose credentials and exposed API are for a loopback-only demo. Production
  authorization, webhook verification, least-privilege DB roles and rate limits
  require explicit work before deployment.

## Verification gate

Run local build/unit/race/vet/lint/security checks, then inspect the exact GitHub
Actions commit for PostgreSQL integration and clean-checkout Compose smoke.
The current verified status is recorded in `AGENTS.md` and `docs/roadmap.md`.
