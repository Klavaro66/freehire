package config

import (
	"os"
	"time"
)

// AutoApply holds the tuning knobs for the unattended application-submission worker
// (cmd/auto-apply). Unlike ApplyForm's fetch (one HTTP call to a public JSON API), one
// attempt here is a full headless-browser session — scan, reconcile, resolve, and maybe
// fill and submit — so its call timeout is an order of magnitude more generous, and the
// lease has to cover the same session, not a single request.
type AutoApply struct {
	BatchSize    int           // claim wave size
	LeaseSeconds int           // how long a claim is held before it can be reclaimed
	MaxAttempts  int           // transient failures before an attempt is dead-lettered
	Concurrency  int           // how many attempts run at once
	MaxPerRun    int           // how much of the queue one run takes; 0 is unbounded
	CallTimeout  time.Duration // bounds a single attempt's sidecar call
	// SidecarURL is the base address of services/auto-apply, the Playwright/Patchright
	// process that does the actual browser work. No default: an unset sidecar is a
	// deploy that has not wired the new service yet, not a value to guess at.
	SidecarURL string
}

// LoadAutoApply reads the worker's tuning from the environment, all optional with
// defaults except SidecarURL. The defaults are deliberately conservative — this is the
// first worker in the fleet that drives a real browser per item, and a submission is a
// real side effect against a third party, unlike a capture's read-only fetch.
func LoadAutoApply() AutoApply {
	a := AutoApply{
		BatchSize:    envInt("AUTO_APPLY_BATCH_SIZE", 20),
		LeaseSeconds: envInt("AUTO_APPLY_LEASE_SECONDS", 300),
		MaxAttempts:  envInt("AUTO_APPLY_MAX_ATTEMPTS", 3),
		Concurrency:  envInt("AUTO_APPLY_CONCURRENCY", 2),
		MaxPerRun:    envInt("AUTO_APPLY_MAX_PER_RUN", 200),
		CallTimeout:  time.Duration(envInt("AUTO_APPLY_CALL_TIMEOUT_SECONDS", 120)) * time.Second,
		SidecarURL:   os.Getenv("AUTO_APPLY_SIDECAR_URL"),
	}
	if a.BatchSize < 1 {
		a.BatchSize = 1
	}
	if a.Concurrency < 1 {
		a.Concurrency = 1
	}
	// The lease must outlast one full browser session, the same reasoning
	// config.LoadApplyForm applies to a single fetch — a lease shorter than the call
	// timeout makes an in-flight attempt reclaimable mid-session, and a lease of 0
	// re-claims a just-failed entry in a tight loop, burning its retry budget in one run.
	if floor := int(a.CallTimeout.Seconds()); a.LeaseSeconds < floor {
		a.LeaseSeconds = floor
	}
	return a
}
