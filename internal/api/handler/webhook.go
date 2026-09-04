package handler

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"

	"github.com/strelov1/freehire/internal/platform/db"
	"github.com/strelov1/freehire/internal/platform/pgconv"
	"github.com/strelov1/freehire/internal/platform/tokencrypt"
)

// webhookHandlers serves the account's single saved-search webhook destination:
// create/rotate, view, enable/disable, delete. cipher is the AES-256-GCM key
// that lets the server recover the stored secret to sign each delivery (see
// internal/platform/config.WebhookSecretKey) — nil disables writing a
// destination (create/rotate/enable/disable/delete are unregistered) while
// GetWebhook stays served either way, reporting "unconfigured". The same
// degrade Connect-Gmail uses for its own token key, split the way its own
// always-available status route is.
type webhookHandlers struct {
	queries *db.Queries
	cipher  *tokencrypt.Cipher
}

func newWebhookHandlers(queries *db.Queries, cipher *tokencrypt.Cipher) *webhookHandlers {
	return &webhookHandlers{queries: queries, cipher: cipher}
}

func (h *webhookHandlers) ready() bool { return h.cipher != nil }

func (h *webhookHandlers) register(api fiber.Router, mw middleware) {
	// Cookie-only, like subscription management: a browser convenience, never an
	// API key — the destination's secret is itself a credential.
	//
	// GET is always registered — like telegramStatus, it reports "unconfigured"
	// rather than 404ing — because the frontend's shared notification-state load
	// calls it alongside telegramStatus/listSubscriptions in one Promise.all, and
	// an unregistered route there would fail that whole load, not just the
	// webhook chip. Reading never needs the cipher (no secret is decrypted), so
	// this is safe with h.cipher nil. Writes genuinely need it — a rotate must
	// encrypt a secret it can later recover — so they stay gated on h.ready(),
	// mirroring Connect-Gmail's OAuth-connect-routes-only-when-configured split.
	api.Get("/me/webhook", mw.cookie, h.GetWebhook)
	if !h.ready() {
		return
	}
	api.Post("/me/webhook", mw.cookie, h.CreateOrRotateWebhook)
	api.Patch("/me/webhook", mw.cookie, h.SetWebhookEnabled)
	api.Delete("/me/webhook", mw.cookie, h.DeleteWebhook)
}

// webhookResponse is the public, secret-free shape of a webhook destination.
type webhookResponse struct {
	URL           string     `json:"url"`
	Enabled       bool       `json:"enabled"`
	CreatedAt     *time.Time `json:"created_at"`
	LastSuccessAt *time.Time `json:"last_success_at"`
	DisabledAt    *time.Time `json:"disabled_at"`
}

func toWebhookResponse(w db.WebhookConfig) webhookResponse {
	return webhookResponse{
		URL:           w.URL,
		Enabled:       w.Enabled,
		CreatedAt:     pgconv.TimePtr(w.CreatedAt),
		LastSuccessAt: pgconv.TimePtr(w.LastSuccessAt),
		DisabledAt:    pgconv.TimePtr(w.DisabledAt),
	}
}

// createdWebhookResponse adds the plaintext secret to the destination metadata.
// It is the response of CreateOrRotateWebhook only — the one and only time the
// secret is revealed; only its encrypted form is persisted, and no other
// endpoint ever returns it again.
type createdWebhookResponse struct {
	webhookResponse
	Secret string `json:"secret"`
}

type createWebhookRequest struct {
	URL string `json:"url"`
}

type setWebhookEnabledRequest struct {
	Enabled bool `json:"enabled"`
}

// generateWebhookSecret returns a high-entropy, URL-safe random secret for
// HMAC-signing webhook deliveries.
func generateWebhookSecret() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// validateWebhookURL rejects anything but an http(s) URL, per the
// webhook-notifications spec's creation-time scheme check.
func validateWebhookURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid webhook url")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fiber.NewError(fiber.StatusBadRequest, "webhook url must be http or https")
	}
	return nil
}

// CreateOrRotateWebhook creates the account's webhook destination, or rotates
// it (new secret, and the given URL) if one already exists — there is exactly
// one per account. The plaintext secret is returned exactly once.
func (h *webhookHandlers) CreateOrRotateWebhook(c *fiber.Ctx) error {
	userID, err := requireUserID(c)
	if err != nil {
		return err
	}
	var in createWebhookRequest
	if err := c.BodyParser(&in); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if err := validateWebhookURL(in.URL); err != nil {
		return err
	}

	secret, err := generateWebhookSecret()
	if err != nil {
		return err
	}
	encrypted, err := h.cipher.Encrypt(secret)
	if err != nil {
		return err
	}

	row, err := h.queries.UpsertWebhookConfig(c.Context(), db.UpsertWebhookConfigParams{
		UserID:          userID,
		URL:             in.URL,
		SecretEncrypted: encrypted,
	})
	if err != nil {
		return err
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"data": createdWebhookResponse{webhookResponse: toWebhookResponse(row), Secret: secret},
	})
}

// GetWebhook returns the authenticated user's webhook destination metadata, or
// null if none is configured. Never includes the secret.
func (h *webhookHandlers) GetWebhook(c *fiber.Ctx) error {
	userID, err := requireUserID(c)
	if err != nil {
		return err
	}
	row, err := h.queries.GetWebhookConfig(c.Context(), userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return c.JSON(fiber.Map{"data": nil})
	}
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"data": toWebhookResponse(row)})
}

// SetWebhookEnabled toggles the destination on/off without rotating its secret
// or URL. A missing destination is a 404.
func (h *webhookHandlers) SetWebhookEnabled(c *fiber.Ctx) error {
	userID, err := requireUserID(c)
	if err != nil {
		return err
	}
	var in setWebhookEnabledRequest
	if err := c.BodyParser(&in); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}

	if in.Enabled {
		if _, err := h.queries.EnableWebhookConfig(c.Context(), userID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	} else if _, err := h.queries.DisableWebhookConfig(c.Context(), userID); err != nil {
		return err
	}

	// A single follow-up read renders the final state and is what actually
	// reports "no destination configured" — EnableWebhookConfig's own
	// pgx.ErrNoRows is swallowed above rather than duplicating that check.
	row, err := h.queries.GetWebhookConfig(c.Context(), userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return fiber.NewError(fiber.StatusNotFound, "no webhook destination configured")
	}
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"data": toWebhookResponse(row)})
}

// DeleteWebhook removes the account's webhook destination entirely. A missing
// destination is a 404.
func (h *webhookHandlers) DeleteWebhook(c *fiber.Ctx) error {
	userID, err := requireUserID(c)
	if err != nil {
		return err
	}
	affected, err := h.queries.DeleteWebhookConfig(c.Context(), userID)
	if err != nil {
		return err
	}
	if affected == 0 {
		return fiber.NewError(fiber.StatusNotFound, "no webhook destination configured")
	}
	return c.SendStatus(fiber.StatusNoContent)
}
