# Auto-apply queue-drain conventions

## Scope
The domain logic behind `cmd/auto-apply`: claim a wave of `auto_apply_queue` rows, assemble
each candidate's known answers, ask a browser driver to resolve and maybe submit the
application, and record the outcome. Pgx/Fiber-free — `Store`, `AnswerSource` and
`SidecarClient` are ports; `cmd/auto-apply` supplies the real implementations
(`dbStore`, `assemblerAnswerSource`, `internal/atsapply.Client`).

## Always true
- **Submits only when every required question is answered.** `SidecarClient.Submit` returns
  `StatusApplied` or `StatusParked` (with the unmapped reasons); there is no partial-fill
  outcome. A required field with no known answer parks the whole attempt — nothing here ever
  guesses.
- **`Park` is not a retry.** A parked attempt needs new data, not another try — it is
  excluded from reclaim (`blocked_at`) rather than counted against `MaxAttempts`. Only a
  genuine transient failure (`Fail`) spends the retry budget.
- **A real submission that fails to record locally is dead-lettered immediately, not
  retried normally.** `recordApplied` forces `Fail(..., maxAttempts=1)` rather than the
  run's configured attempts budget when `Store.Submit` errors after the sidecar already
  reported `StatusApplied` — the ordinary retry path would eventually re-arm the row for
  reclaim and risk calling the browser driver again for a job already applied to, which the
  "never submit twice" invariant forbids outright. See `recordApplied`'s doc comment.
- **`SidecarClient.Submit` takes the whole `Claimed`, not its individual fields** — mirroring
  `internal/applyform.Fetcher.Fetch`'s own reasoning: what a submission needs is not the same
  for every provider (Greenhouse/Ashby need `ExternalID`, not just `JobURL`), so the seam
  should not grow a parameter every time a provider needs one more piece of the claim.
- **`AnswerSource` supplies identity/work-authorization facts only (Tier A/B).** There is no
  Tier C (LLM-drafted answer) yet — a question outside that set always parks. The real
  implementation (`cmd/auto-apply`'s `assemblerAnswerSource`) wraps
  `internal/candidateprofile.Assembler`, the same one the browser extension's autofill path
  reads, so a value a person sees in a form and a value this worker resolves against can
  never diverge.

## How it works
`Run` wires `outbox.RunPool` over `Store.Claim`, mirroring `internal/applyform`'s own
`cmd/capture-apply-form` runner shape. `process` per claimed item: assemble answers → call
`SidecarClient.Submit` → map the result to `Store.Submit` (success; composes
`LockJobForApply` + `MarkJobApplied` + queue retirement in one transaction, in
`cmd/auto-apply/store.go`) / `Store.Park` (unresolved) / `Store.Fail` (transient error).
`RunStats.Degraded` treats a parked attempt as healthy (the system correctly declined to
guess, not a fault) — only a dead letter or a run that failed everything counts.

What populates `auto_apply_queue` is out of scope for this package entirely — see
`openspec/changes/auto-apply-worker/design.md`. `Run` only ever claims what is already
there.
