## Why

Nothing in the system can submit a job application without the candidate present. `internal/applyform` only describes a form; `internal/autofillagent` (via the browser extension) only fills whatever page the candidate is already looking at. A candidate matched to a job still has to open the posting themselves before anything moves. A spike (2026-09-02) confirmed both that this is buildable — a real Greenhouse posting's rendered form can be scanned, reconciled and filled headlessly with Patchright — and that the naive approach (trusting `internal/applyform`'s stored API-only schema) would silently under-fill: the live DOM carried 36 fields against 17 the stored schema declares.

## What Changes

- Add a new outbox-style queue, `auto_apply_queue`, holding one row per (user, job) attempt, drained with the same lease/attempts mechanics `internal/outbox.RunPool` already provides elsewhere in the codebase.
- Add `cmd/auto-apply`, a run-once-and-exit cron worker that claims queue rows, assembles the candidate's existing profile answers via `autofillProfile()`/`profileFields()` (already built for the extension autofill path — no new answer source), and delegates the actual browser work to a sidecar.
- Add `services/auto-apply`, a new Python/Patchright sidecar (precedent: `services/pii-filter`) that scans a job's live rendered application-form DOM, reconciles it against the ATS's own question API where one exists (Greenhouse, Ashby; Lever has none), resolves each field against the answers it was given, and submits the application **only if every required field resolved** — otherwise it touches nothing on the page and reports which fields it could not answer and why.
- On a successful submission, the worker records it through the existing `jobtracking.MarkJobApplied` call — the same path a manual apply takes — so `user_jobs`/`application_events` never diverge between an automated and a manual application.
- On an unresolved (parked) attempt, the queue row is marked with the unmapped reasons; `user_jobs.stage` is deliberately left untouched, since "parked, needs more answers" is not part of that table's controlled stage vocabulary.

Explicitly **not** part of this change (captured in the design doc, `docs/superpowers/specs/2026-09-02-auto-apply-worker-design.md`):
- What populates `auto_apply_queue` (a candidate action vs. a standing per-user rule) — the worker only drains it.
- Persisting the DOM-scan/reconciled form schema — this change scans live, on every run, and stores nothing new; `internal/applyform`'s own stored schema is not read by this worker.
- Automatically retrying a parked attempt once the candidate supplies the missing answer.
- Routing a parked attempt into the extension's `RunAgentAutofill` as a guided manual finish.

## Capabilities

### New Capabilities
- `auto-apply-submit`: unattended submission of a job application through a queued (user, job) attempt — claiming the queue, resolving the candidate's known answers against the job's live rendered form, and submitting only when every required field is answered, recording the outcome through the existing application-tracking path.

### Modified Capabilities
(none — `apply-form-capture` and `extension-autofill` are unchanged; this worker reads neither)

## Impact

- **New table**: `auto_apply_queue` (migration).
- **New Go worker**: `cmd/auto-apply`, plus its outbox-consumer package (naming TBD in design.md).
- **New service**: `services/auto-apply` (Python, Playwright/Patchright), a sidecar the Go worker calls over HTTP — not a Go package, same category as `services/pii-filter`.
- **Existing code touched, not modified in behavior**: `autofillProfile()`/`profileFields()` (read-only reuse) and `jobtracking.MarkJobApplied` (called, not changed).
- **Dependencies**: introduces Playwright + Patchright into the deploy surface for the first time; no existing Go dependency changes.
- **Operational**: a new deployable service and a new cron worker to run alongside the existing fleet (`cmd/capture-apply-form` et al.).
