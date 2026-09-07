# Codex working contract

- Work diff-first. Read the current task, repository-local instructions, project memory/map when available, and the current diff before broad exploration.
- Keep scope narrow. Do not refactor neighboring code or add abstractions unless the requested behavior or a demonstrated risk requires it.
- Find one authoritative local implementation pattern before generating equivalent code. Use the existing HTTP handlers, PostgreSQL transactions, and manual wiring in cmd/ as local patterns.
- Batch 2–4 related edits when they form one coherent change.
- Prefer deterministic local tools for mechanical correctness: formatter, compiler, targeted tests, linters, security tools, git, and gh.
- Match verification to risk: targeted checks for low risk; package/module + lint for medium risk; full relevant race/integration/security/performance checks only for high risk.
- Use fail-fast verification: cheap checks first, expensive checks once after the change batch is coherent.
- Do not reread unchanged modules unless the current change depends on them.
- Recall persistent memory only when it can prevent rediscovery or a repeated mistake. Use index/preview retrieval first and fetch only the few full records that materially affect the task.
- Treat memory as a cache. Current code, tests, CI, ADRs, and project-local docs outrank recalled memory.
- Use a cheaper capable model for mechanical implementation. Escalate mainly for architecture, subtle concurrency, persistence semantics, distributed consistency, complex bugs, and final high-risk review.
- Keep delegated work atomic, with explicit scope and completion criteria; do not use extra agents for linear work. This clause does not itself request delegation.
- Record durable decisions and verified root causes, not long reasoning transcripts or transient logs. Record project decisions in versioned docs; do not write to external persistent memory without explicit user authorization.
- End a long session at a completed functional slice and resume from a compact handoff/project state when old tool history is no longer useful.
- Keep final reports concise: what changed / checks / risks / next step.
- Load detailed dev-standards guidance only for the active concern; do not preload unrelated database, migration, API, observability, performance, security, or architecture documents. If unavailable locally, do not invent their contents.

## Project context

- Before each work part, remind the user of **one** recommended model and reasoning effort, with a short reason. Do not claim the active model changed automatically.
- Read `docs/roadmap.md` for the current phase and `docs/architecture.md` / `docs/data-model.md` only when the change depends on their decisions.
- Architecture: modular monolith, manual constructor injection, one Go module. No DI framework, generic repository, or unnecessary services.
- Use `golang-how-to` when available for Go work; load only the applicable guidance for the active concern. Explicit user instructions take precedence over skill defaults.
- Keep commit messages conventional; never add AI coauthor/signature trailers.
- Never equate a timeout or retry exhaustion with a definitive supplier refusal. Unknown results retain the same supplier and request ID.
- Keep supplier storage independent from application storage. Count effects in both supplier databases in reliability tests.
- Do not report planned features or unexecuted tests as completed.

## Current handoff

Phases 0 and 1a are complete. Phase 1b implements the order/payment/delivery flow, PostgreSQL migrations, durable supplier A, and integration tests. Local build, unit/race tests, and vet have passed. PostgreSQL integration and Compose checks are pending CI verification; do not mark them passed without inspecting the run for the exact commit.

Next: finish phase 1b CI validation and review; then phase 2, GPT-6 Astra / high. Add 50-event races, independent worker-process competition, and controlled crash recovery. Supplier B, fault injection, reconciliation and large-catalog measurements are not implemented.

Environment observed during phase 0: local Go is 1.26.3; `docker` was not found on PATH. GitHub CLI is authenticated as `krav01` when network permission is available. Recheck these facts before relying on them. Keep Go caches in the session's writable `work/` outside the repository if global caches are sandbox-blocked; never commit them. Do not claim Docker-backed checks passed without running them.
