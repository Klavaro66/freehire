// Command backfill-experience-dates fills experience_employments' structured
// period_start_year/month and period_end_year/month columns (migration 0135) from the
// free-text period_start/period_end columns they are replacing, then exits.
//
// Every write path going forward writes the structured columns directly (see
// internal/candidate/experience); this is the one-off pass for rows written before that
// code shipped. A label that parses (internal/candidate/perioddate.Parse: "2024",
// "October 2018", "2023-09", ...) fills exactly what it says. A label that is empty or
// reads as "not ended" (Present/Current/...) is left unset — that is a real absence, not
// a parse failure, and papering over it with a fabricated date would be worse than
// leaving it blank. Only genuinely garbled text (non-empty, not a present label, and
// still unparseable) falls back to the row's own created_at year: an approximate date
// about a real row reads better to its owner than an empty one. Rare in practice — real
// data is "2024"/"October 2018"-shaped — but a free-text field accepts anything.
//
// Idempotent and safe to stop and resume: SetExperienceEmploymentBackfilledDates is
// guarded to rows still missing both structured periods, so a row already filled (by
// this worker or by the ordinary write paths once deployed) is never touched again.
// Needs only DATABASE_URL.
//
//	go run ./cmd/backfill-experience-dates
package main

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/strelov1/freehire/internal/candidate/perioddate"
	"github.com/strelov1/freehire/internal/platform/db"
	"github.com/strelov1/freehire/internal/platform/worker"
)

func main() { worker.Main(run) }

func run() int {
	ctx, _, pool, cleanup, err := worker.Bootstrap(context.Background())
	if err != nil {
		log.Printf("database: %v", err)
		return 1
	}
	defer cleanup()

	q := db.New(pool)
	rows, err := q.ListExperienceEmploymentDatesForBackfill(ctx)
	if err != nil {
		log.Printf("backfill-experience-dates: list: %v", err)
		return 1
	}
	if len(rows) == 0 {
		log.Print("backfill-experience-dates: nothing to do")
		return 0
	}
	log.Printf("backfill-experience-dates: %d rows to fill", len(rows))

	var filled, fellBack int
	lastLog := time.Now()
	for i, row := range rows {
		start := parseOrFallback(row.PeriodStart, row.CreatedAt.Time)
		end := parseOrFallback(row.PeriodEnd, row.CreatedAt.Time)
		if isFallback(row.PeriodStart, start) || isFallback(row.PeriodEnd, end) {
			fellBack++
		}
		startYear, startMonth := dateToParams(start)
		endYear, endMonth := dateToParams(end)
		n, err := q.SetExperienceEmploymentBackfilledDates(ctx, db.SetExperienceEmploymentBackfilledDatesParams{
			ID:              row.ID,
			PeriodStartYear: startYear, PeriodStartMonth: startMonth,
			PeriodEndYear: endYear, PeriodEndMonth: endMonth,
		})
		if err != nil {
			log.Printf("backfill-experience-dates: row %s after %d filled: %v", row.ID, filled, err)
			return 1
		}
		filled += int(n)

		if time.Since(lastLog) >= time.Minute {
			log.Printf("backfill-experience-dates: progress %d/%d", i+1, len(rows))
			lastLog = time.Now()
		}
		select {
		case <-ctx.Done():
			log.Printf("backfill-experience-dates: cancelled after %d filled, resume by re-running", filled)
			return 1
		default:
		}
	}
	log.Printf("backfill-experience-dates: done, filled=%d (fell back to created_at year for %d unparseable labels)", filled, fellBack)
	return 0
}

// parseOrFallback reads one free-text period label. An empty or present-reading label
// ("Present", "Current", ...) means the period genuinely was not stated — nil, no
// fallback. A label that parses is used as-is. Anything else (non-empty, not a present
// label, still unparseable) falls back to createdAt's year — see the package doc.
func parseOrFallback(raw string, createdAt time.Time) *perioddate.PeriodDate {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || perioddate.IsPresentLabel(trimmed) {
		return nil
	}
	if d, ok := perioddate.Parse(trimmed); ok {
		return d
	}
	return &perioddate.PeriodDate{Year: createdAt.Year()}
}

// isFallback reports whether parseOrFallback took the created_at-year fallback path for
// raw, purely for the run's own summary log — it does not affect what gets written.
func isFallback(raw string, got *perioddate.PeriodDate) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || perioddate.IsPresentLabel(trimmed) || got == nil {
		return false
	}
	_, ok := perioddate.Parse(trimmed)
	return !ok
}

func dateToParams(d *perioddate.PeriodDate) (year pgtype.Int4, month pgtype.Int2) {
	if d == nil {
		return year, month
	}
	year = pgtype.Int4{Int32: int32(d.Year), Valid: true}
	if d.Month != 0 {
		month = pgtype.Int2{Int16: int16(d.Month), Valid: true}
	}
	return year, month
}
