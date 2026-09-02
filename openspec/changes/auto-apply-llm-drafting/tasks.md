## 1. Per-user LLM attribution for cmd/auto-apply

- [ ] 1.1 Extract `userLLM` (the `llmkey.Resolver.For` + `llm.Client.As` composition in `internal/handler/user_llm.go`) into `internal/llmkey`, renamed to fit that package (e.g. `Bind`); update `internal/handler`'s call sites to the moved function; no behavior change
- [ ] 1.2 Update `internal/llmkey/scope_test.go`'s allowlist to admit `cmd/auto-apply` by name alongside the existing `cmd/server` exemption, with a one-line comment carrying the same reasoning
- [ ] 1.3 Wire `cmd/auto-apply/main.go` to construct an `llmkey.Resolver` and bind an `internal/llm.Client` per attempt via the moved `Bind`, tagged `feature:auto-apply-drafting` (new tag constant)

## 2. Sensitive-field gate

- [ ] 2.1 Port `freehire-apply/internal/drafting`'s `isSensitive` keyword list into `internal/atsapply` (compensation, work authorization/visa sponsorship, EEO/demographic categories) as a pure, unit-tested function over a question's label text
- [ ] 2.2 Unit test: every category in the ported list is caught; a representative set of non-sensitive labels (language proficiency, referral source, "why this company") are not

## 3. Grounding source

- [ ] 3.1 Add a grounding-context builder that reads `experience.Store.ListAtoms`/`ListEmployments` for the attempt's candidate and filters to `Provenance.Publishable()` atoms only
- [ ] 3.2 Unit test: an `agent_inferred` atom is excluded from the grounding context even when it would otherwise answer the question well

## 4. Drafter

- [ ] 4.1 Define the `Drafter` interface (`Draft(ctx, question MergedField, grounding GroundingContext) (answer string, ok bool, err error)`) in `internal/atsapply`
- [ ] 4.2 Implement the real `Drafter` over `internal/llm.Client` with a structured-output schema (grounded answer, or an explicit "no basis" signal — never an empty string standing in for "nothing to say")
- [ ] 4.3 Wire the drafter into `Resolve`'s (or a new orchestration step's) handling of an unmapped, non-sensitive, free-text-kind field: draft, then re-run the existing "must match an offered option, where the field has any" check before accepting the value
- [ ] 4.4 Unit test the wiring with a fake `Drafter`: a sensitive field never reaches `Draft` at all; a drafted value that fails the offered-options check still parks; a `Drafter` returning `ok=false` still parks; a successful, grounded draft fills the field

## 5. Verification and documentation

- [ ] 5.1 Manual smoke check (throwaway, not committed) against the same live Greenhouse posting `auto-apply-worker`'s task 7.1 used, confirming at least one previously-unmapped free-text question now drafts a grounded answer — no fill/submit click, matching the discipline already established
- [ ] 5.2 Update `internal/atsapply/AGENTS.md`'s custom-question gap note (currently describes only the visa-sponsorship label match) to describe the drafting fallback, the sensitive-gate, and what still parks (ungroundable non-sensitive questions, all sensitive questions, any drafted value that fails the offered-options check)
- [ ] 5.3 Note the new LLM-call cost and the `feature:auto-apply-drafting` tag in `internal/config`'s `LoadAutoApply` doc comment or `internal/atsapply/AGENTS.md`, wherever this worker's other real-side-effect costs are already flagged
