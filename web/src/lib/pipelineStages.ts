// The five stages a posting passes through before it's searchable, in order — this is a
// real pipeline, not a curated list, so the numbering is load-bearing rather than
// decorative. Written from docs/architecture.md and the root AGENTS.md; kept in one file
// so the page's copy and its illustration registry (pipeline/StageIllustration.svelte)
// cannot name a stage that has no picture, or draw one nothing describes.
export type StageKey = 'ingest' | 'derive' | 'enrich' | 'deduplicate' | 'reindex';

export type Stage = {
  key: StageKey;
  n: string;
  title: string;
  blurb: string;
  detail: string;
};

export const PIPELINE_STAGES: Stage[] = [
  {
    key: 'ingest',
    n: '01',
    title: 'Ingest',
    blurb:
      'We crawl company career pages, ATS platforms and boards directly, several times a day — so you see a posting the day it goes up, not weeks later from a stale aggregator.',
    detail:
      "Most of the catalogue comes straight from an employer's own ATS or careers page, not from a copy of a copy. Each source runs on its own schedule so one slow platform never holds up the rest, and a board that keeps failing backs off instead of being hammered every run.",
  },
  {
    key: 'derive',
    n: '02',
    title: 'Derive',
    blurb:
      'Seniority, skills, location and remote/onsite are pulled out by a fixed dictionary, not guessed — so filtering by "Senior · Go · Remote" actually narrows to that.',
    detail:
      'This step never invents a value: it looks a term up in a closed dictionary and tags the posting if it finds a match, and emits nothing for anything it doesn\'t recognize. That is deliberate — a facet built on a guess degrades quietly, and quietly is the one way a filter is allowed to fail here.',
  },
  {
    key: 'enrich',
    n: '03',
    title: 'Enrich',
    blurb:
      "An LLM reads the full posting for the things a dictionary can't catch — what the role actually involves, benefits, visa mentions — checked before it's shown.",
    detail:
      'This runs from a queue, not inline with the crawl, so a slow model call never holds up ingest. Every result is validated against a fixed schema before it reaches you; one that fails validation is dropped rather than shown half-formed.',
  },
  {
    key: 'deduplicate',
    n: '04',
    title: 'Deduplicate',
    blurb:
      "The same opening often sits on three or four boards at once. We collapse those into one listing, so your search results aren't full of repeats.",
    detail:
      "Postings cluster by role and company, and near-duplicate copies collapse into one — including the aggregator's copy of a job we already have straight from the source. You see one listing; where it's actually posted is still there for you to check.",
  },
  {
    key: 'reindex',
    n: '05',
    title: 'Reindex',
    blurb:
      "The deduped catalogue rebuilds into the search index you actually query, then swaps in at once — search is never down for a rebuild.",
    detail:
      'The index is rebuilt from scratch and swapped in atomically, so a search you run mid-rebuild reads the old, complete index or the new one — never a half-written one in between.',
  },
];
