## Context

See `proposal.md` for motivation and `docs/superpowers/specs/2026-09-02-auto-apply-worker-design.md`
for the brainstormed design this formalizes (spike findings, architecture diagram, rationale
for each boundary). This document restates only what a task breakdown needs to be unambiguous.

Existing pieces this change builds on, unchanged in behavior:
- `internal/outbox.RunPool` — the claim/process/lease loop `internal/applyform`'s
  `cmd/capture-apply-form` already runs on. `Store` there is a three-method port (`Claim`,
  `Save`/success, `Fail`/retry — this change's shape differs slightly, see Decisions) backed
  by generated sqlc queries.
- `autofillProfile()` / `profileFields()` (`internal/handler/autofill_profile.go`,
  `autofill_agent.go`) — the candidate-profile map already assembled for the extension
  autofill path (identity + work-authorization facts). This change's only answer source.
- `internal/jobtracking.MarkJobApplied` — the existing write path a manual apply already
  takes (`applied_at`, `stage`, `application_events`, `jobs.applied_count`, under
  `LockJobForApply`).
- `services/pii-filter` — the one existing precedent in this repo for a non-Go sidecar
  service, i.e. the deployment/ops shape this change's sidecar follows.

## Goals / Non-Goals

**Goals:**
- A working, narrow, end-to-end path: queued attempt in → submitted application or a named
  reason it could not be, out.
- Reuse every existing piece that already does part of this job (profile data, tracking
  write path, outbox mechanics) rather than parallel-inventing any of them.

**Non-Goals** (design-level, beyond what proposal.md already excludes):
- Concurrency/throughput tuning. `RunPool`'s existing batch/lease/concurrency parameters are
  reused as-is; no new tuning knobs are designed here.
- Any UI surface. This change has no candidate-facing screen — `auto_apply_queue` rows are
  written and read by backend code only in this change.
- Anti-detection hardening beyond Patchright's default configuration. The spike measured
  that default config leaves several headless tells unaddressed; closing them is deferred
  (see Risks).

## Decisions

### The sidecar is called synchronously per attempt, not batched

The Go worker calls `services/auto-apply` once per claimed row and blocks for the result
(bounded by a call timeout, matching `applyform`'s `_CALL_TIMEOUT_SECONDS` convention) rather
than handing the sidecar a batch. **Alternative considered**: batch a wave to the sidecar in
one call. Rejected for this change: a submission is a real side effect against a third party
with its own captcha/rate-limit behavior per attempt; a batched call would need its own
partial-failure protocol (which items in the batch succeeded) that duplicates what the outbox
already gives the Go side for free. One call per attempt keeps failure attribution trivial —
revisit if per-attempt latency makes the queue fall behind in practice.

### The sidecar's HTTP contract is the entire Go/Python boundary

`POST /submit {job_url, provider, answers} → {status: applied|parked|error, unmapped?, reason?}`
is the only interface. The Go side never receives or inspects field-level data — it treats
`unmapped` as an opaque payload to store, not to interpret. **Alternative considered**: expose
a richer API (e.g., a separate `/scan` step the Go side could inspect before deciding to
fill). Rejected: nothing on the Go side needs to make a decision from that data yet ("routing
a parked attempt to the extension" is explicitly out of scope for this change), so a richer
contract would be speculative surface with no current caller.

### `auto_apply_queue`'s `Store` port mirrors `applyform.Store` but is not the same interface

Same shape (`Claim`, a success write, a failure write), but a distinct Go interface in a new
package, because the success write differs in substance: `applyform.Save` persists a captured
form; this change's success write calls `jobtracking.MarkJobApplied` and marks the queue row
done in the same transaction (mirroring `LockJobForApply`'s existing serialization, so a
double-claim of the same row can't double-submit — see the spec's "never twice" requirement).
**Alternative considered**: generalize `applyform.Store` into a shared outbox-with-side-effect
interface both packages implement. Rejected as premature — two implementations is not yet a
pattern worth abstracting (AGENTS.md: no infrastructure before a concrete second need beyond
this one), and `internal/outbox.RunPool` itself is already the shared part.

### `parked` is a queue-row status, never a `user_jobs.stage`

Per the spec's requirement that an unresolved attempt not move the tracked stage: the queue
row's own `status` column (`pending` / `blocked` / `done` / `failed`) is the only place
"could not resolve" is recorded. `user_jobs` is touched exactly once, on `done`, through the
existing `MarkJobApplied` call — never for `blocked`. This keeps the controlled stage
vocabulary (`internal/userjob/stages.go`) exactly as it is today; nothing here adds a value to
it.

### DOM is scanned live, per attempt; nothing about the form is persisted by this change

Already decided in the brainstormed design (see linked doc) and restated here because it
drives the data model: `auto_apply_queue` stores no field-level schema, only the attempt's
identity, status, and (on `blocked`) the `unmapped` reasons returned for that one run. A
later attempt for the same job re-scans from scratch.

## Migration Plan

- One additive migration: `auto_apply_queue` (new table only; no existing table altered).
- `services/auto-apply` is a new deployable unit with no prior version — first deploy has no
  live-traffic cutover to sequence, unlike a change to an already-serving path.
- `cmd/auto-apply` ships disabled-by-default in practice: it only ever processes rows that
  exist in `auto_apply_queue`, and nothing in this change writes to that table (population is
  out of scope), so deploying it is inert until a future change starts enqueueing.
- No rollback beyond the normal revert-and-redeploy — the new table and worker have no
  consumers elsewhere in the codebase to leave dangling.

## Risks / Trade-offs

- **[Risk]** Datacenter IP reputation may cause blocks independent of browser-fingerprint
  cleanliness (carried over from the spike; no mitigation designed in this change) →
  **Mitigation**: none yet; read `blocked`/`error` rates with this in mind before assuming
  they are answer-resolution gaps. Revisit (proxy strategy) only if observed in practice.
- **[Risk]** Patchright's default configuration still fails several non-`navigator.webdriver`
  headless-detection checks (measured against a public bot-detection page in the spike) →
  **Mitigation**: none in this change; `services/auto-apply` ships with Patchright defaults.
  Further context/launch hardening is deferred until real submit volume shows whether it
  matters.
- **[Risk]** Lever renders a captcha on every posting, so every Lever attempt parks in this
  change's scope (no captcha-solving path exists) → **Mitigation**: expected and acceptable
  for v1 per the spec's "submit only when fully resolved" requirement; solving it
  interactively is the deferred extension-fallback work.
- **[Trade-off]** Live DOM-scanning on every attempt instead of persisting a reconciled
  schema costs a browser session per attempt even for a job seen before → accepted for this
  change's narrow scope; `internal/applyform`'s stored-schema path remains available to build
  on later without this change needing to anticipate it.
