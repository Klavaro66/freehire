-- name: InsertTelegramPost :execrows
-- Crawl write path: store a fetched post once. ON CONFLICT DO NOTHING makes
-- re-crawling idempotent — a stored post (pending, done, or dead-lettered) is
-- never reset. extracted_at is non-NULL when the ingest prefilter already
-- decided the post holds no vacancy, so it is recorded but never queued.
INSERT INTO telegram_posts (channel, msg_id, text, links, posted_at, extracted_at)
VALUES (sqlc.arg(channel), sqlc.arg(msg_id), sqlc.arg(text), sqlc.arg(links), sqlc.arg(posted_at), sqlc.arg(extracted_at))
ON CONFLICT (channel, msg_id) DO NOTHING;

-- name: ClaimTelegramPosts :many
-- Claim a batch of pending posts by stamping claimed_at. SKIP LOCKED lets
-- concurrent workers take disjoint rows; the lease predicate reclaims posts whose
-- worker died (stale claimed_at), so no separate reaper process is needed.
-- Oldest post first so a backlog drains in posting order.
WITH claimable AS (
    SELECT channel, msg_id
    FROM telegram_posts
    WHERE extracted_at IS NULL
      AND failed_at IS NULL
      AND (claimed_at IS NULL
           OR claimed_at < now() - make_interval(secs => sqlc.arg(lease_seconds)::int))
    ORDER BY posted_at
    FOR UPDATE SKIP LOCKED
    LIMIT sqlc.arg(batch_size)
)
UPDATE telegram_posts p
SET claimed_at = now()
FROM claimable c
WHERE p.channel = c.channel AND p.msg_id = c.msg_id
RETURNING p.channel, p.msg_id, p.text, p.links, p.posted_at;

-- name: MarkTelegramPostExtracted :exec
-- Completion: the post was processed (jobs written, or no vacancy found). Run in
-- the same transaction as the extracted jobs' UpsertJob calls.
UPDATE telegram_posts
SET extracted_at = now()
WHERE channel = sqlc.arg(channel) AND msg_id = sqlc.arg(msg_id);

-- name: RecordTelegramPostFailure :one
-- Count a failed attempt: bump attempts, record the error, and dead-letter (set
-- failed_at) once attempts reach the max. The lease (claimed_at) is intentionally
-- left in place — its expiry gates the retry to a later run and doubles as the
-- crash reaper, so a failed post is never reprocessed within the same run.
UPDATE telegram_posts
SET attempts   = attempts + 1,
    last_error = sqlc.arg(last_error),
    failed_at  = CASE
                     WHEN attempts + 1 >= sqlc.arg(max_attempts)::int THEN now()
                     ELSE NULL
                 END
WHERE channel = sqlc.arg(channel) AND msg_id = sqlc.arg(msg_id)
RETURNING attempts, failed_at;
