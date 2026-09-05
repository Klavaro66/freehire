## 1. Shared `perioddate` package

- [x] 1.1 Create `internal/candidate/perioddate/perioddate.go`: `type PeriodDate struct { Year, Month int }` (`Month == 0` = year-only), `Sanitize()` (year clamped 1900–2100, month clamped 0/1–12 else 0), `Format() string` (e.g. "Mar 2021", "2018"). Implemented as `type Date struct` (not `PeriodDate`) to avoid the `perioddate.PeriodDate` stutter — see `golang-naming` skill's anti-stutter rule; same shape and behavior the task describes.
- [x] 1.2 Add `Parse(s string) (*PeriodDate, bool)` to the same package: port `period_sort.go`'s existing parsing logic to return a `*Date`. Recognizes the same formats it did.
- [x] 1.3 Add `IsPresentLabel(s string) bool` — ported.
- [x] 1.4 `Date.MarshalJSON`/`UnmarshalJSON`: marshal always emits `{"year":Y,"month":M}` (omit `month` when 0); unmarshal accepts that object, a legacy JSON string (delegates to `Parse`), **or a bare JSON number** (found necessary in task 3.3 — mirrors the old `verbatimString` shim's defense against a model returning a raw int where an object was asked for). A `Date` whose fields carry no `json:` tags would make the schema in task 3.3 ask the model for `{"Year":...}` (capitalized, untagged Go field names) instead of `{"year":...}` — `Date.Year`/`Date.Month` carry explicit lowercase tags for this reason (see task 3.3's note).
- [x] 1.5 Unit tests (`perioddate_test.go`): `Parse`, `Format`, `FormatRange`, `Sanitize`, `MarshalJSON`, `UnmarshalJSON` (object/legacy-string/bare-number/garbage), round-trip, `IsPresentLabel`.
- [x] 1.6 Added `perioddate` to `internal/platform/arch/layering/blocks.go`'s `candidate` block.
- [x] (found necessary, not in original list) Added `perioddate.FormatRange(start, end *Date, current bool) string` — the " – "-joining helper every consumer that used to hold two pre-formatted strings needed (typst's `daterange`, the assistant tools' period line, `ExperienceBankView.svelte`'s display line) now that both sides are structured.

## 2. `experience_employments`: schema + backend

- [x] 2.1 Migration `0135_experience_employments_structured_dates.sql`: additive `period_start_year/month`, `period_end_year/month` (nullable), month-implies-year CHECK constraints, replaced `experience_employments_user_idx` to order on the new columns natively.
- [x] 2.2 `internal/platform/db/queries/experience.sql` updated (List/Get/Find/Create/Update/FillBlanks all read/write the new columns; `FillExperienceEmploymentBlanks` fills a period as a whole year+month pair exactly when its year is currently NULL). Added `ListExperienceEmploymentDatesForBackfill`/`SetExperienceEmploymentBackfilledDates` for the backfill worker (task 2.8). `make sqlc` run.
- [x] 2.3 `experience.go`: `Employment.Start`/`End` are `*perioddate.Date`; `Sanitize()` calls `perioddate.Sanitize`.
- [x] 2.4 `repository.go`: **found necessary, deviates from the task's framing** — sqlc now generates a *distinct row type per query* (`ListExperienceEmploymentsRow`, `CreateExperienceEmploymentRow`, ...) instead of the one shared `db.ExperienceEmployment`, because none of the six employment queries still select the full table's columns (the old `period_start`/`period_end` text columns are gone from all of them). Introduced a package-local `employmentRow` struct as `Repository`'s one return shape; each `queriesRepository` method converts its own sqlc row type into it. `dateToDB`/`dateFromDB` convert between `*perioddate.Date` and the two `pgtype.Int4`/`pgtype.Int2` columns.
- [x] 2.5 `import_resume.go`: `Current` is copied from `role.Current` (no more deriving it from an end-label string).
- [x] 2.6 **Deviates from the task**: `period_sort.go`/`period_sort_test.go` are deleted, but a small `sort.go`/`sort_test.go` replaces them — `Store.ListEmployments` still re-sorts in Go (not just relying on the SQL `ORDER BY`), because `fakeRepo` (the unit-test double) returns rows in plain insertion order with no ordering of its own; the comparator itself is now a few lines of direct `*perioddate.Date` comparison (no parsing left to do).
- [x] 2.7 `assistant_experience_tools.go` and `assistant_profile_tool.go` (a second call site the task didn't name, found by the compiler): both now use `perioddate.FormatRange`.
- [x] 2.8 `cmd/backfill-experience-dates`: implemented as a single-pass worker (fetches every still-unfilled row via `ListExperienceEmploymentDatesForBackfill`, writes via `SetExperienceEmploymentBackfilledDates`) rather than the id-range-chunked pattern `cmd/backfill-slug-folded` uses — that pattern exists for `jobs`-scale tables (millions of rows over a far-ahead-running id sequence); `experience_employments` is per-candidate data, orders of magnitude smaller. Idempotent via the write query's own guard. Entry added to `AGENTS.md`'s worker table and `.gitignore` (required — `internal/platform/arch`'s `TestEveryCmdBinaryIsGitignored` caught the missing entry).
- [x] 2.10 `go test ./internal/candidate/experience/...` green.
- [x] 2.9 explicitly **NOT done in this change** — the design says it runs "only after 2.8 has run and the new code is deployed", i.e. it is a follow-up action for whoever operates the deploy, not something to ship pre-written in this PR.

## 3. `resumeextract`

- [x] 3.1 `structured.go`: `Experience.Start`/`End` and `Education.Year` are `*perioddate.Date` (the task's fallback clause — "leave `Education.Year` as-is" — did not apply; the same-shaped change was clean). `Experience` gains `Current bool`.
- [x] 3.2 `sanitizeExperience`/`sanitizeEducation` call `perioddate.Sanitize`.
- [x] 3.3 **The schema needed no hand-written change** — `requestSchema()` derives the JSON schema from the `Structured` Go type by reflection (`internal/platform/llmschema`), so changing the field types alone changed the derived schema's `start`/`end`/`year` shape to a nested object automatically (verified live via a temporary schema dump, then pinned with `TestRequestSchema_DateFieldsAreLowercase`). What *did* need a manual fix: `perioddate.Date`'s fields had no `json:` tags, so the schema asked for `{"Year":...,"Month":...}` (capitalized Go field names) instead of `{"year":...,"month":...}` — fixed by tagging `Date` itself (see task 1.4).
- [x] 3.4 `resumeextract.go`'s `systemPrompt` rewritten: `start`/`end`/`year` are now described as `{"year":int,"month":int 1-12, omit if unstated}` objects, with an explicit instruction to set `current: true` and `end: null` for an ongoing role instead of inventing an end year.
- [x] 3.5 `go test ./internal/candidate/resumeextract/...` green, including the new schema-shape test and the rewritten `flexdecode_test.go`/`visibility_test.go`/`professional_test.go` fixtures. Manual spot-check against real CVs **not done** — no live LLM credentials in this environment; flagged as an open follow-up in the PR description.

## 4. `cv` (document model + renderer)

- [x] 4.1 `cv.go`: `ExperienceItem.Start`/`End`, `EducationItem.Start`/`End`, `Certification.Year` are `*perioddate.Date`.
- [x] 4.2 Their `sanitize*` functions call `perioddate.Sanitize`.
- [x] 4.3 `seed.go`: copies the now-structured fields straight through; also now copies `Current` (a field `ExperienceItem` already had but `Seed` never populated, since `resumeextract.Experience` had no `Current` to copy from before task 3.1).
- [x] 4.4 `renderer.go`: `renderPayload` re-declares `Experience`/`Education`/`Certifications` with `perioddate.Date`-formatted string fields (Go's JSON promotion rules let the shallower, re-declared field win over the one promoted from the embedded `Document`) — the 9 `.typ` templates are untouched, confirmed by inspection (no `.typ` file was edited).
- [x] 4.5 `go test ./internal/candidate/cv/...` green, including a new non-typst-dependent `TestRenderPayloadFormatsStructuredDatesToStrings` asserting the JSON payload's `start`/`end`/`year` are plain strings matching the pre-existing format (e.g. `Current: true` → `"Present"`). The typst-dependent PDF-text tests (`TestTypstRendererProducesExtractableATSText` etc.) still pass but self-skip in this environment (no `typst` binary) — they don't assert on date text specifically, so this doesn't cover PDF-level verification; see task 6.4.

## 5. Frontend

- [ ] 5.1 Regenerate `web/src/lib/generated/contracts.ts` (`make gen-contracts`) — `Experience`/`ExperienceItem`'s `start`/`end` become the generated `Date`-shaped interface.
- [ ] 5.2 Hand-edit `web/src/lib/types.ts`'s `ExperienceEmployment` (`experience.Employment` is not in `gen-contracts`) to match: `start`/`end` become `{ year: number; month?: number } | undefined`.
- [ ] 5.3 New shared component (e.g. `web/src/lib/components/PeriodDateInput.svelte`): `<input type="month">` bound to the structured value, plus a "I don't remember the month" toggle that swaps in a plain year `<input type="number">`; emits/accepts the `{year, month?}` shape (or `undefined`).
- [ ] 5.4 `web/src/lib/components/ExperienceBankView.svelte`: replace the plain text start/end inputs (employment add/edit form) with `PeriodDateInput`; update the display line that concatenates `employment.start`/`end` to format the structured value instead.
- [ ] 5.5 `web/src/lib/components/cv/CvSectionForm.svelte`: replace the plain text start/end inputs (experience and education entries) with `PeriodDateInput`.
- [ ] 5.6 `web/src/lib/api.ts` (and any other call site constructing/reading `ExperienceEmployment`/`Experience`/`ExperienceItem` start/end as a plain string) updated to the structured shape.
- [ ] 5.7 `pnpm --dir web check` (svelte-check) passes; `pnpm --dir web test` (vitest) passes.

## 6. Verification

- [x] 6.1 `go build ./...`, `go vet ./...`, `go vet -tags=integration ./...` all pass.
- [x] 6.2 `go test ./...` passes (all packages, including `-tags=integration` compile check). The real `-tags=integration` *run* against Postgres (`internal/platform/db`'s `TestExperienceFillBlanksNeverOverwrites` etc.) was rewritten for the new columns but not executed in this environment — no Docker Postgres available at this step; flagged for CI/manual follow-up.
- [ ] 6.3 Manual pass against the running dev stack (needs the frontend from section 5 first).
- [ ] 6.4 Manual pass: render a CV to PDF (or its HTML preview) for a profile with mixed-precision dates; confirm the rendered text is unchanged. Partially covered by 4.5's JSON-payload test; the actual PDF/typst step is still open (no `typst` binary in this environment).
- [ ] 6.5 Run `cmd/backfill-experience-dates` against representative data and confirm the `created_at`-year fallback triggers for at least one deliberately-unparseable row. Covered at the unit level (`main_test.go`'s `TestParseOrFallback`/`TestIsFallback`); not yet run against a real seeded database.
