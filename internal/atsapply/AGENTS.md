# ATS browser-driver conventions

## Scope
Drives a headless Chrome (`chromedp`) against one job's live application-form page: scan the
rendered DOM, reconcile it against the platform's own declared schema
(`internal/applyform.Form`, reused — not re-fetched), resolve the merged fields against a
candidate's known answers, and fill + submit only when every required question is answered.
Implements `internal/autoapply.SidecarClient` — the one caller is `cmd/auto-apply`.

**This package is chromedp, in-process — not a Python/Patchright sidecar.** The OpenSpec
change this package belongs to originally proposed one; a follow-up spike found chromedp + a
real Chrome install matched or beat Patchright on every automation-detection signal measured,
at the cost of no second language, process, or deploy artifact. See
`openspec/changes/auto-apply-worker/design.md`'s "chromedp, not a Python/Patchright sidecar"
decision for the measurements and its caveats.

## Always true
- **The DOM decides what exists; required is the union of DOM and API.** A field the API
  declares but the DOM never renders is dropped (filling it would write into a control that
  isn't on the page); a field the DOM renders but the API never declares is kept regardless.
  Required is NOT DOM-only, though: a live Greenhouse posting rendered `country` as required
  with no HTML `required` attribute at all — the API's own required flag is what catches
  that. See `reconcile.go`'s `TestReconcile_RequiredIsTheUnionOfDOMAndAPI`.
- **Never guesses an answer.** A select/checkbox's answer must match one of the platform's
  own offered option labels, or the field parks. An optional field with no known answer is
  left alone entirely (neither filled nor reported) — nothing here drafts text for it.
- **A DOM-only live scan is built for Greenhouse only.** Lever always parks on its captcha
  (`requiresCaptcha`, a static per-provider check, not DOM-based detection) before any
  fetcher or browser is touched. Every other provider (Ashby, and anything reached in the
  future) reconciles against `applyform.Form` alone — `mergedFromAPIOnly` — and a fully
  resolved form for one of them still parks rather than being submitted through a fill path
  never built or verified. Widening the live DOM-scan to another provider is a real gap to
  close, not a design decision to defend.
- **File uploads are not resolved.** `resolve.go`'s `answerKeyFor` never matches a `file`
  kind field — a required résumé/cover-letter upload always parks. There is no artifact
  (which stored CV, which version) wired through yet.
- **Custom employer questions rarely resolve, even when relevant data exists.**
  `answerKeyFor` is an ID-based lookup: it matches Greenhouse's own standardized field names
  (`first_name`, `email`, ...) but a numeric `question_NNNNN` id can never match it, even when
  `candidateprofile` holds the relevant fact (e.g. `visa_sponsorship_needed`) — nothing here
  reads a *label* to connect the two. Measured against a live posting (task 7.1): 5 of 7
  unmapped required fields were exactly this shape. Label/keyword matching for the handful of
  near-universal custom questions (work authorization, sponsorship) is the highest-value next
  increment, not a currently-open task in this change's scope.
- **The fill/submit path (`fill.go`, `browser.go`) is the least-verified part of this
  package.** No unit tests exercise it — a real browser session cannot be faked usefully, and
  no test asserts a real submission against a live board (that would spam a real employer).
  Correctness rests on a single spike's measurements and the reference implementation's own
  documented rules, not on this package's own live testing, until real submit volume proves
  otherwise. Two known gaps: no assertion that a react-select-style field's typed value
  actually "stuck" before submitting (the reference implementation calls this out as its own
  main failure mode), and one field shape scanned with an empty id on a real posting that
  this package does not yet name correctly.

## How it works
`Client.Submit`: captcha short-circuit → fetch the platform's schema via
`applyform.Fetchers` → (Greenhouse only) launch a browser, render the page, `ScanGreenhouseForm`
→ `Reconcile` → `Resolve` → if `Plan.FullyResolved()`, `fillAndSubmit`; else return
`StatusParked` with `Plan.Unmapped`. `fillAndSubmit` fills every resolved field, clicks
Greenhouse's submit button, and waits for a text-based confirmation or refusal marker —
matching neither is reported as unconfirmed (an error), never silently treated as success.

`stealthAllocatorOptions` (`browser.go`) is the whole anti-detection surface: headless plus
`disable-blink-features=AutomationControlled`, the one flag the spike measured flipping
`navigator.webdriver` to `false`. Datacenter IP reputation is a separate, unaddressed risk —
see design.md's Risks.
