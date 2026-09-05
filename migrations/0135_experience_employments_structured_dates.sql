-- Structured period dates for experience_employments (see internal/candidate/perioddate
-- and the structured-experience-dates change). Additive: the existing period_start/
-- period_end text columns stay untouched here so cmd/backfill-experience-dates has
-- something to read from — a follow-up migration drops them once the backfill and the
-- code that reads these new columns have both landed.
--
-- Plain nullable integers, not a SQL date: a bare "2024" on a CV is real evidence with a
-- real precision, and a date type would force it to lie about a day (and, for a
-- year-only value, a month) nobody stated. Year is NOT NULL-able at the column level (a
-- period can be entirely absent, in which case both columns are NULL) but month alone is
-- nullable within a present period, meaning "no month stated".
ALTER TABLE public.experience_employments
    ADD COLUMN period_start_year integer,
    ADD COLUMN period_start_month smallint,
    ADD COLUMN period_end_year integer,
    ADD COLUMN period_end_month smallint;

ALTER TABLE public.experience_employments
    ADD CONSTRAINT experience_employments_period_start_month_check
        CHECK (period_start_month IS NULL OR period_start_month BETWEEN 1 AND 12),
    ADD CONSTRAINT experience_employments_period_end_month_check
        CHECK (period_end_month IS NULL OR period_end_month BETWEEN 1 AND 12),
    -- A month with no year would have no meaning to sort or display by.
    ADD CONSTRAINT experience_employments_period_start_month_needs_year_check
        CHECK (period_start_month IS NULL OR period_start_year IS NOT NULL),
    ADD CONSTRAINT experience_employments_period_end_month_needs_year_check
        CHECK (period_end_month IS NULL OR period_end_year IS NOT NULL);

-- Replaces experience_employments_user_idx (0047): the old index ordered period_start
-- lexicographically, which is wrong whenever a candidate's roles mix a bare year with a
-- month-and-year label — the very bug cmd/backfill-experience-dates and this column pair
-- exist to fix. NULLS LAST keeps an employment with no start date sorting after every
-- dated one, matching period_sort.go's old "unknown sorts last" behavior.
DROP INDEX IF EXISTS public.experience_employments_user_idx;

CREATE INDEX experience_employments_user_idx
    ON public.experience_employments (
        user_id,
        is_current DESC,
        period_start_year DESC NULLS LAST,
        period_start_month DESC NULLS LAST
    );
