package telegram

import (
	"context"
	"log"
	"time"
)

// PendingPost is a claimed telegram_posts row awaiting extraction.
type PendingPost struct {
	Channel  string
	MsgID    int64
	Text     string
	PostedAt time.Time
	Links    []Link
}

// Extractor classifies a post and extracts its vacancies via an LLM. The kind
// steers the prompt (board: expect one vacancy; authored: expect 0..N). The
// result is not trusted — the runner validates before persisting.
type Extractor interface {
	Extract(ctx context.Context, text string, kind Kind) (Extraction, error)
}

// ResolvedJob is a fully-identified vacancy parsed by following a post's outbound link to
// its destination site (e.g. career.habr.com). Unlike an ExtractedJob it carries its own
// source identity, so it is stored under the destination platform, not "telegram".
type ResolvedJob struct {
	Source      string
	ExternalID  string
	URL         string
	Title       string
	Company     string
	Location    string
	Description string
	Remote      bool
	PostedAt    *time.Time
	WorkMode    string
}

// LinkResolver turns a post's outbound links into fully-identified jobs by fetching and
// parsing their destination pages. It returns the jobs from every link a destination
// adapter matched; a non-nil error means matched links existed but all failed (a transient
// failure worth retrying), while no matched link yields (nil, nil) so the caller falls back
// to the LLM. Per-link parse skips and failures are the resolver's concern to log.
type LinkResolver interface {
	Resolve(ctx context.Context, links []Link) ([]ResolvedJob, error)
}

// ExtractStore is the persistence boundary of the extraction worker. Complete
// writes the extracted jobs through the canonical job upsert and marks the post
// extracted in one transaction; Fail counts a failed attempt (dead-lettering at
// the attempt cap is the store's concern).
type ExtractStore interface {
	Claim(ctx context.Context, leaseSeconds, batchSize int32) ([]PendingPost, error)
	Complete(ctx context.Context, post PendingPost, jobs []ExtractedJob) error
	// CompleteLinks writes link-resolved jobs (each under its own source identity) and
	// marks the post extracted, the same transactional shape as Complete.
	CompleteLinks(ctx context.Context, post PendingPost, jobs []ResolvedJob) error
	Fail(ctx context.Context, post PendingPost, errMsg string) error
}

// ExtractStats summarizes one extraction run.
type ExtractStats struct {
	Processed int // posts completed (jobs written or none found)
	Jobs      int // vacancies written
	Failed    int // posts whose extraction failed this run
}

// Extraction queue tuning. The lease must outlive the slowest plausible LLM
// call; its expiry doubles as the crash reaper (see the enrichment runner).
const (
	leaseSeconds = 600
	batchSize    = 50
)

// ExtractRunner drains one batch of pending posts: claim, extract, validate,
// persist. A post whose payload is invalid or whose LLM call fails is failed —
// the store retries it once (on a later run, after the lease expires) and then
// dead-letters it; an invalid payload is never persisted.
type ExtractRunner struct {
	Extractor Extractor
	Store     ExtractStore
	Kinds     map[string]Kind // channel → kind, from sources/telegram.yml
	Links     LinkResolver    // optional; resolves outbound job links to full vacancies
}

// Run processes one claimed batch and returns its stats. A post whose links a destination
// adapter resolves is stored from those (deterministic) jobs and the LLM is skipped; any
// other post takes the LLM path.
func (r ExtractRunner) Run(ctx context.Context) (ExtractStats, error) {
	var stats ExtractStats

	posts, err := r.Store.Claim(ctx, leaseSeconds, batchSize)
	if err != nil {
		return stats, err
	}

	for _, post := range posts {
		linkJobs, err := r.resolveLinks(ctx, post)
		if err != nil {
			// Matched links existed but all failed — fail the post so the lease retries it.
			log.Printf("telegram: resolve links %s/%d failed: %v", post.Channel, post.MsgID, err)
			stats.Failed++
			if ferr := r.Store.Fail(ctx, post, err.Error()); ferr != nil {
				return stats, ferr
			}
			continue
		}
		if len(linkJobs) > 0 {
			if err := r.Store.CompleteLinks(ctx, post, linkJobs); err != nil {
				return stats, err
			}
			stats.Processed++
			stats.Jobs += len(linkJobs)
			continue
		}

		extraction, err := r.Extractor.Extract(ctx, post.Text, r.kind(post.Channel))
		if err == nil {
			err = extraction.Validate()
		}
		if err != nil {
			log.Printf("telegram: extract %s/%d failed: %v", post.Channel, post.MsgID, err)
			stats.Failed++
			if ferr := r.Store.Fail(ctx, post, err.Error()); ferr != nil {
				return stats, ferr
			}
			continue
		}

		if err := r.Store.Complete(ctx, post, extraction.Jobs); err != nil {
			return stats, err
		}
		stats.Processed++
		stats.Jobs += len(extraction.Jobs)
	}
	return stats, nil
}

// resolveLinks follows a post's outbound links to full vacancies, returning nil when no
// resolver is configured or the post has no links.
func (r ExtractRunner) resolveLinks(ctx context.Context, post PendingPost) ([]ResolvedJob, error) {
	if r.Links == nil || len(post.Links) == 0 {
		return nil, nil
	}
	return r.Links.Resolve(ctx, post.Links)
}

// kind resolves a channel's configured kind, defaulting to board for a post
// whose channel has since left sources/telegram.yml (the safer, single-vacancy prompt).
func (r ExtractRunner) kind(channel string) Kind {
	if k, ok := r.Kinds[channel]; ok {
		return k
	}
	return KindBoard
}
