## 1. Data model

- [x] 1.1 Add migration creating `auto_apply_queue` (`user_id`, `job_id`, `status` — `pending`/`blocked`/`done`/`failed`, `attempts`, `last_error`, `claimed_until`, `unmapped` jsonb nullable, `created_at`)
- [x] 1.2 Add sqlc queries: claim a leased batch, mark done (within the same statement/transaction as the application-tracking write — see 5.1), mark blocked with `unmapped`, mark failed with attempts/dead-letter, matching `internal/applyform`'s existing query shapes
- [x] 1.3 Run `make sqlc` and commit generated `internal/db` output

## 2. Go queue store (`internal/autoapply`)

- [x] 2.1 Define the `Store` port (`Claim`, `Submit` (success), `Park` (unresolved), `Fail` (retry)) per design.md's decision that this is its own interface, not `applyform.Store` reused
- [x] 2.2 Implement `Store` against the generated queries from 1.2 (`cmd/auto-apply/store.go`, mirroring `cmd/capture-apply-form`'s `dbStore` — the implementation lives beside the binary, not inside the domain package, matching that existing split)
- [x] 2.3 Unit test `Store`'s claim/park/fail behavior as an `integration`-tagged test against a real Postgres (`internal/db/auto_apply_queue_integration_test.go`), matching how `internal/applyform`'s queries are tested

## 3. Sidecar HTTP client (Go side)

- [x] 3.1 Define a `SidecarClient` interface (`Submit(ctx, jobURL, provider, answers) (Result, error)`) and an HTTP implementation with a bounded call timeout (config, default matching `applyform`'s `_CALL_TIMEOUT_SECONDS` convention)
- [x] 3.2 Unit test the HTTP implementation against a local test server covering the three response shapes (`applied`, `parked`, error) and a timeout

## 4. `cmd/auto-apply` worker

- [x] 4.1 Implement the per-attempt process function: assemble `answers` via `autofillProfile()`/`profileFields()`, call `SidecarClient.Submit`, map the result to `Store.Submit`/`Park`/`Fail`
- [x] 4.2 Wire `internal/outbox.RunPool` + `Store` + `SidecarClient` into `cmd/auto-apply/main.go`, following `cmd/capture-apply-form`'s bootstrap shape (`internal/worker` conventions: `DATABASE_URL`, exit codes, batch/lease/concurrency env vars). Along the way, extracted `autofillProfile`/`profileFields` out of `internal/handler` into a new `internal/candidateprofile` package — they were unexported and handler-scoped, so `cmd/auto-apply` could not call them as originally assumed. Both the extension autofill path and this worker now share one `Assembler` (`internal/handler`'s tests moved with the code they were testing).
- [x] 4.3 Unit test the process function with a mock `Store` and mock `SidecarClient`: `applied` → `Submit` called; `parked` → `Park` called with reasons; transient error → `Fail` called (plus two cases the design surfaced: an answer-assembly failure never reaches the sidecar, and a lost post-submit record forces an immediate dead-letter rather than the normal retry path, to keep the "never twice" requirement)

## 5. Application-tracking integration

- [x] 5.1 Compose `db.Queries.MarkJobApplied` (the same statement `jobtracking.QueriesRepository.MarkApplied` runs, called directly rather than through the slug-based `jobtracking.Service`, since the queue already carries `job_id`) and marking the queue row done under one transaction/lock (`LockJobForApply`), so a double-claim cannot double-submit — this is what the spec's "never twice" requirement rests on. `EventSource` is `appevent.SourceSystem`: the platform acting on the candidate's behalf, not something they typed.
- [x] 5.2 Test: a pair that already has a submitted application is not double-counted on a second `Submit` (`cmd/auto-apply/store_integration_test.go`, real Postgres — verifies exactly one `applications` row and `jobs.applied_count` staying at 1)

## 6. Python sidecar (`services/auto-apply`)

- [ ] 6.1 Scaffold the service (dependencies, entrypoint, HTTP server) following `services/pii-filter`'s existing shape as precedent
- [ ] 6.2 Implement DOM-scan per provider (Greenhouse, Ashby, Lever) as a pure function over rendered HTML — no browser in this function's tests
- [ ] 6.3 Implement reconciliation against the ATS's question API where one exists (Greenhouse, Ashby); Lever has none and is DOM-only
- [ ] 6.4 Implement answer resolution: match reconciled fields against the incoming `answers` map (identity/work-authorization only); every required field resolved or the attempt reports `parked` with per-field reasons
- [ ] 6.5 Implement fill + submit via Patchright: attach-then-overwrite ordering, react-select selection assertion, upload verification, submission confirmed/refused by text marker (per the spike's findings and the reference implementation's measured rules)
- [ ] 6.6 Implement captcha detection (Lever renders one on every posting) → always reports `parked` with a `requires_captcha` reason, never attempts a blind submit
- [ ] 6.7 Implement the `POST /submit` endpoint tying 6.2-6.6 together, returning `{status, unmapped?, reason?}`
- [ ] 6.8 Unit tests over fixture HTML per provider for the scan → reconcile → resolve chain (mirroring the reference implementation's `--mock` approach), no live network in CI

## 7. Verification and documentation

- [ ] 7.1 Manual smoke test against a live public Greenhouse and Ashby posting (Lever expected to always park on the captcha) confirming the full path end to end — not part of CI, same status as `applyform`'s own live-fetch code today
- [ ] 7.2 Add `internal/autoapply/AGENTS.md` and a `services/auto-apply` note documenting the queue contract, the Go/sidecar boundary, and the "submits only when fully resolved" invariant
- [ ] 7.3 Add `cmd/auto-apply` to the root `AGENTS.md` worker table/list alongside the other cron workers
