# internal/search

The Meilisearch index topology, the incremental drain into it, saved searches, the facet snapshot, and intent parsing.

**Layer 6 of 8.**

May import: `platform`, `dict`, `ai`, `identity`, `candidate`, `job` — and itself.

Must NOT import: `application`, `engage`, `ingest`, `api`.

`application` share this layer, and the ban runs both ways: two blocks that can see each other are one block under two names.

Both directions are enforced. `depguard` in `.golangci.yml` fails on the
offending import line; `internal/platform/arch/layering` holds the same table and
reports the whole graph at once, including imports that exist only in test files.

## Packages

`facetsnapshot` `savedsearch` `search` `searchdrain` `searchintent` `similarjobs`
