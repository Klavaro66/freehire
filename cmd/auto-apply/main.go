// Command auto-apply drains auto_apply_queue: for each queued (candidate, job) attempt it
// resolves the candidate's known profile answers against the job's live application form,
// through internal/atsapply's headless browser, and submits only when every required
// question is answered. What populates the queue is out of scope for this worker — see
// openspec/changes/auto-apply-worker/design.md — it only ever drains what is already there.
//
// It exits non-zero when the run had any failures or dead letters, so cron can alert.
package main

import (
	"context"
	"log"

	"github.com/strelov1/freehire/internal/atsapply"
	"github.com/strelov1/freehire/internal/autoapply"
	"github.com/strelov1/freehire/internal/blobstore"
	"github.com/strelov1/freehire/internal/candidateprofile"
	"github.com/strelov1/freehire/internal/config"
	"github.com/strelov1/freehire/internal/cv"
	"github.com/strelov1/freehire/internal/db"
	"github.com/strelov1/freehire/internal/resume"
	"github.com/strelov1/freehire/internal/screeninganswers"
	"github.com/strelov1/freehire/internal/sources"
	"github.com/strelov1/freehire/internal/worker"
)

func main() {
	worker.Main(run)
}

func run() int {
	ctx, cfg, pool, cleanup, err := worker.Bootstrap(context.Background())
	if err != nil {
		log.Printf("database: %v", err)
		return 1
	}
	defer cleanup()

	acfg := config.LoadAutoApply()

	// blobStore is nil when S3_* is unconfigured, and that is fine here: the only résumé
	// read this worker makes is candidateprofile's Structured(), which reads the parsed
	// structure from Postgres and never touches object storage (resume.Store.Structured
	// reads only its repo). Object storage only matters once this package attaches a
	// résumé file to a submission, which it does not do yet — see resolve.go's file-kind
	// handling and internal/atsapply/AGENTS.md's known gaps.
	blobStore, err := blobstore.New(blobstore.Config{
		Endpoint:  cfg.S3Endpoint,
		Bucket:    cfg.S3Bucket,
		AccessKey: cfg.S3AccessKey,
		SecretKey: cfg.S3SecretKey,
	})
	if err != nil {
		log.Printf("blobstore: %v", err)
		return 1
	}

	queries := db.New(pool)
	cvStore := cv.NewStore(cv.NewQueriesRepository(queries))
	resumeStore := resume.New(blobStore, resume.NewQueriesRepository(queries))
	screeningAnswersSvc := screeninganswers.New(screeninganswers.NewQueriesRepository(queries))
	// The same four sources, in the same precedence order, internal/handler's
	// extension-autofill path already resolves a candidate's profile through — see
	// internal/candidateprofile's package doc for why this must be the one Assembler.
	answers := assemblerAnswerSource{
		assembler: candidateprofile.NewAssembler(cvStore, resumeStore, queries, screeningAnswersSvc),
	}

	// The same HTTP client the crawl and internal/applyform's own capture worker use: the
	// Greenhouse/Ashby endpoints internal/atsapply reuses via applyform.Fetchers are the
	// platforms' own public job-board APIs, so its user agent, timeouts and size caps are
	// exactly right here too.
	sidecar := atsapply.NewClient(sources.NewClient())

	stats, err := autoapply.Run(ctx, newDBStore(pool), answers, sidecar, autoapply.RunOptions{
		BatchSize:    acfg.BatchSize,
		LeaseSeconds: acfg.LeaseSeconds,
		MaxAttempts:  acfg.MaxAttempts,
		Concurrency:  acfg.Concurrency,
		MaxPerRun:    acfg.MaxPerRun,
		CallTimeout:  acfg.CallTimeout,
	})
	if err != nil {
		log.Printf("auto-apply: %v", err)
		return 1
	}

	log.Printf("auto-apply done: applied=%d blocked=%d failed=%d dead_lettered=%d",
		stats.Applied, stats.Blocked, stats.Failed, stats.DeadLettered)
	// Deliberately not worker.ExitCode: see autoapply.RunStats.Degraded for why a parked
	// attempt is not a fault this worker's exit code should alert on.
	if stats.Degraded() {
		return 1
	}
	return 0
}
