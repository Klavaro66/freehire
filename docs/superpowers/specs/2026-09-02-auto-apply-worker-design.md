# Auto-apply worker: submit a job application unattended

Date: 2026-09-02
Status: proposed

## Problem

`internal/applyform` captures the questions an ATS form asks, and `internal/autofillagent` /
`RunAgentAutofill` can fill whatever page the user is currently looking at through the
browser extension — but both need the candidate present. Nothing in the system can take a
job the candidate has already been matched to and actually submit an application without
them opening a tab.

A spike (2026-09-02, throwaway, not committed) confirmed two things against a live
Greenhouse posting (`webflow`, job `7951430`):

- The rendered application form carries 36 fields; the `?questions=true` API `applyform`
  reads today declares 17. The gap is exactly what a third-party reference implementation
  (ApplYourself) measured: `country`, `candidate-location` (declared as `location` — a name
  mismatch), and the whole EEOC block (`gender`, `hispanic_ethnicity`, `veteran_status`,
  `disability_status`) are rendered but never declared.
- `patchright` (a Playwright fork that patches CDP-level automation leaks) flips
  `navigator.webdriver` from `true` to `false` at zero code cost over stock Playwright, but a
  public bot-detection page (bot.sannysoft.com) still fails several other headless tells
  (`window.chrome` object missing, permissions API shape, `HEADCHR_*` checks) under both
  drivers. Patchright closes one specific, commonly-checked signal; it is not a complete
  stealth solution out of the box.

## Scope of this design

This is the first of four dependent pieces toward full auto-apply (DOM-scan+reconcile
persistence, richer answer resolution, and the extension fallback for parked applications are
the other three). It is deliberately narrow:

**In scope:** a worker that, given a job and a candidate's existing profile answers, either
submits a real application or leaves it untouched and says why not.

**Out of scope, decided explicitly:**
- **What populates the queue** — whether a candidate clicks "Auto Apply" on one posting, or a
  standing per-user rule queues anything above a score threshold. The worker only drains
  whatever is there.
- **DOM-scan persistence** — `internal/applyform`'s stored schema exists for "the consumer
  that fills a form rather than describing it" (its own docs), implying a reconciled schema
  saved once and reused. This design scans the live DOM inside the same browser session used
  to fill it, on every run, and stores nothing new. Cheaper to build first; revisit if
  repeated scanning becomes a cost or reliability problem.
- **Retrying a parked application after the candidate adds the missing answer** — for now
  that is "someone flips the queue row back to pending"; no automatic re-trigger.
- **Routing a parked application into the extension's `RunAgentAutofill`** as a guided
  manual finish. Natural next step, not this piece.

## Architecture

```
cmd/auto-apply (Go, cron, run-once-and-exit)
  └─ internal/outbox.RunPool over auto_apply_queue
        for each claimed (user_id, job_id):
          profile := autofillProfile(user_id) → profileFields()   [already exists]
          job     := job's apply_form URL + provider
          POST http://auto-apply-sidecar/submit {job_url, provider, answers}
          ─────────────────────────────────────────────────────────
          services/auto-apply (Python, Patchright)                 [new sidecar,
            scan DOM → reconcile w/ API schema (GH/Ashby only) →     services/pii-filter
            resolve fields against `answers` → all required          is the precedent]
            resolved? → fill → submit → verify by text marker
                      : else → touch nothing
          ─────────────────────────────────────────────────────────
          ← {status: "applied"} | {status: "parked", unmapped: [...]} | error

          "applied"  → jobtracking.MarkJobApplied(user_id, job_id)   [same path as a
                        (same call a manual apply makes)               manual apply —
                     → queue row: done                                 keeps user_jobs /
          "parked"   → queue row: blocked, unmapped reasons stored     application_events
                        user_jobs.stage untouched (not part of         from diverging]
                        the controlled stage vocabulary)
          error      → retry via the queue's own lease/attempts,
                        same mechanics as cmd/capture-apply-form
```

### Why a live scan instead of the persisted `applyform` store

`internal/applyform` today stores only the API-declared questions, which the spike showed is
incomplete for Greenhouse and is DOM-only for Lever entirely (no question API at all). Reading
that store as-is would silently under-fill required fields. Rather than building the
persistence-and-reconciliation layer first, the sidecar scans the real DOM at fill time, in
the same browser session — it already has to open the page to fill it, so the scan is nearly
free there. The persisted, reconciled schema `applyform` anticipated stays a real
improvement (faster, cacheable, reusable by the job-page display that already reads this
store) — just not a prerequisite for a working worker.

### The Go/Python boundary

Everything ATS-specific — DOM structure, react-select verification, upload confirmation,
submission text markers, Patchright/stealth configuration — lives in the Python sidecar,
mirroring the reference implementation's own module boundary (`plan.py` decides, `fill.py`
executes, nothing decides twice). The Go side never inspects a form field; it only assembles
`answers` from data it already has and interprets a status enum back.

`answers` sent to the sidecar is exactly `profileFields()`'s output — the same map
`autofillagent.Profile` already builds for the extension path (full name, contacts, work
authorization, salary/notice-period facts). No new answer source for this piece; the resolver
inside the sidecar only ever sees Tier A (identity) / Tier B (work authorization) data. A form
whose required fields extend past that set always comes back `parked` in v1 — there is no
Tier C (LLM-drafted answer) yet.

## Data model

New table, `auto_apply_queue`, shaped after `applyform`'s capture queue:

- `user_id`, `job_id` — composite identity of one attempt.
- `status` — `pending` / `blocked` / `done` / `failed`. (Not `user_jobs.stage` — see above.)
- `attempts`, `last_error`, `claimed_until` — the same lease/retry fields
  `internal/outbox.RunPool` already expects, so the worker is a thin `cmd/` wrapper, not new
  concurrency-control code.
- `unmapped` (jsonb, nullable) — the `[{id, label, required, reason}]` list from a `parked`
  result, for whatever later reads this to route into the extension fallback.

Population (`INSERT`) and requeue (`status` reset to `pending`) are both out of scope here, as
decided above — whatever populates the queue writes directly to this table.

## Error handling

- **Sidecar unreachable / times out** — treated as `failed`, retried through the queue's
  lease mechanics, same shape as a transient `applyform` capture failure.
- **Board requires a captcha** (Lever renders one on every posting) — the sidecar reports
  `parked` with a `requires_captcha` reason rather than attempting a blind submit; this board
  therefore parks every single time in v1, which is expected and not a bug to chase here.
  Solving it interactively is extension-fallback territory (out of scope).
  Ashby/Greenhouse aren't known to gate on one, but a run that hits one there reports the same
  way rather than guessing.
- **A required field the API declares but the DOM never renders, or vice versa** — the DOM is
  authoritative (per the spike and the reference implementation's own measurement); a
  declared-but-absent field is simply not in the resolver's input at all.
- **Partial fill state on failure** — the sidecar only ever fills after confirming every
  required field resolves; a `parked` result never touches the page, so there is no partial
  fill to clean up. A `failed` result (crash mid-fill, after the resolved-check passed) may
  leave a half-filled, unsubmitted browser page, but the browser context is torn down either
  way — nothing persists past one sidecar process.

## Testing

- Go side: `cmd/auto-apply`'s queue-claim loop tested against a mock sidecar HTTP client, no
  browser in unit tests — same discipline as `applyform`'s runner tests.
- Sidecar: unit tests over fixture HTML/DOM per provider (mirroring ApplYourself's `--mock`
  approach) for the scan → reconcile → resolve chain, with no live network. A separate,
  manually-run check against live postings (as the spike did) stays outside CI, the same way
  `applyform`'s live-fetch code is exercised manually today.
- No test asserts a real submission succeeds against a live board — that would spam a real
  employer's pipeline. Confirmation-marker logic is tested against captured fixture HTML of a
  real confirmation page, not a live run.

## Open risks (carried forward, not solved here)

- **Datacenter IP reputation** is a separate signal from browser fingerprint; Patchright does
  not address it. The worker running from the same hosts as other cron jobs may get blocked
  independently of how clean the browser looks. No mitigation designed yet — noting it so
  `parked`/`failed` rates are read with this in mind rather than assumed to be answer-resolution
  gaps.
- **Patchright's default configuration leaves several non-webdriver headless tells present**
  (measured in the spike). Further context/launch hardening may be needed once real submit
  volume shows whether this matters in practice.
