# internal/ai

Model-backed features and the spend attribution around them: enrichment, embeddings, the in-app assistant, speech, credits. The LLM client itself is NOT here — it is transport, and lives in `platform`.

**Layer 3 of 8.**

May import: `platform`, `dict` — and itself.

Must NOT import: `identity`, `candidate`, `job`, `application`, `search`, `engage`, `ingest`, `api`.

`identity` share this layer, and the ban runs both ways: two blocks that can see each other are one block under two names.

Both directions are enforced. `depguard` in `.golangci.yml` fails on the
offending import line; `internal/platform/arch/layering` holds the same table and
reports the whole graph at once, including imports that exist only in test files.

## Packages

`aiarchetype` `assistant` `autofillagent` `browsertools` `credits` `embed` `enrich` `llmkey` `speech`
