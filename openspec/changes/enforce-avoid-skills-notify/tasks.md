## 1. SQL: batch-fetch excluded skills by user id

- [ ] 1.1 Add `ListUserProfilesExcludedSkills` (or similarly named) query to `internal/db/queries/user_profiles.sql`: `SELECT user_id, excluded_skills FROM user_profiles WHERE user_id = ANY(sqlc.arg(user_ids)::bigint[])`.
- [ ] 1.2 Run `make sqlc` and confirm the generated code compiles (`go build ./...`).

## 2. userprofile: batch accessor

- [ ] 2.1 Add `ExcludedSkillsByUserIDs(ctx context.Context, userIDs []int64) (map[int64][]string, error)` to the `internal/userprofile.Repository` interface and its `QueriesRepository` implementation, backed by the new query from task 1.1. A user id with no profile row is simply absent from the returned map.
- [ ] 2.2 Write a table-driven unit test in `internal/userprofile/userprofile_test.go` (fake repository) or `internal/userprofile/userprofile_integration_test.go` (real DB) covering: multiple user ids with distinct excluded-skill sets, a user id with an empty `excluded_skills`, and a user id with no profile row at all (absent from the result map).

## 3. notify: per-subscriber avoid-skills exclusion in matching

- [ ] 3.1 In `internal/notify/match.go`'s `Runner.match`, after `ListActiveSubscriptions`, collect the distinct `user_id`s across all active subscriptions and call `ExcludedSkillsByUserIDs` once to build a `map[int64][]string`.
- [ ] 3.2 Thread that map into `matchQuery` (new parameter) and, in its `for _, hit := range res.Hits { for _, s := range subs {...} }` loop, skip recording `(s.ID, hit.ID)` when `hit.Skills` intersects `excludedByUser[s.UserID]` (case-insensitive set membership — skills are already canonical/lowercased from the dictionary, so a direct string compare is sufficient; confirm against `internal/skilltag` normalization before assuming no extra normalization step is needed).
- [ ] 3.3 Update `internal/notify/match.go`'s doc comment (lines 14-21) to describe the new avoid-skills gate alongside the existing `start_at` gate.

## 4. Tests: internal/notify

- [ ] 4.1 Write a failing test in `internal/notify` (fake store/searcher, following the existing pattern in `internal/notify/notify_test.go`) for: a job matching the shared query but carrying a skill in one subscriber's `excluded_skills` is not recorded as a match for that subscriber.
- [ ] 4.2 Extend/add a test for: two subscriptions sharing one canonical query, only one subscriber has the matching skill excluded — the job is recorded for the other subscriber and not for the excluding one (verifies the fan-out stays per-subscriber, not per-query).
- [ ] 4.3 Add a test for: a subscriber's `excluded_skills` is updated between two matching passes — the pass after the update stops recording new matches for that skill (no subscription/saved-search mutation required).
- [ ] 4.4 Add a test for: a subscriber with no profile row (absent from the batch map) matches normally — avoid-skills absence must not break or skip otherwise-valid matches.
- [ ] 4.5 Run `go test ./internal/notify/... ./internal/userprofile/...` and confirm all new and existing tests pass.

## 5. Docs

- [ ] 5.1 Update `docs/agents/notifications.md`'s "Always true" bullet list with a short note that `notify` matching also gates on the subscriber's live `excluded_skills`, evaluated per-subscriber without an extra search call — so a future reader doesn't reintroduce a per-subscriber Meili filter that would break the O(distinct queries) property.

## 6. Verification

- [ ] 6.1 `gofmt -l .` prints nothing for changed files.
- [ ] 6.2 `go vet ./...` and `go test ./...` pass.
- [ ] 6.3 `go vet -tags=integration ./...` passes.
- [ ] 6.4 Manually sanity-check with `go run ./cmd/notify` against a local DB seeded with a subscription whose query matches a job carrying a skill in the test account's `excluded_skills`, confirming no digest is produced for that job.
