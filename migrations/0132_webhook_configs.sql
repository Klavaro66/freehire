-- One webhook destination per account; the FK cascade purges it on user delete.
-- secret_encrypted is AES-256-GCM ciphertext (base64), never plaintext — see
-- internal/platform/tokencrypt. disabled_at is set when a delivery gets a 410
-- from the destination (see internal/engage/webhooknotify); NULL while enabled.
CREATE TABLE IF NOT EXISTS webhook_configs (
    user_id          BIGINT      PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    url              TEXT        NOT NULL,
    secret_encrypted TEXT        NOT NULL,
    enabled          BOOLEAN     NOT NULL DEFAULT true,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_success_at  TIMESTAMPTZ,
    disabled_at      TIMESTAMPTZ
);
