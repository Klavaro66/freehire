//go:build integration

// Integration tests for the saved-search webhook HTTP flow against a real
// Postgres: create/rotate returns the plaintext secret once, get/list never
// does, patch toggles without rotating, delete removes the destination, and
// every endpoint requires the session cookie. Run with:
// go test -tags=integration ./internal/api/handler/
package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/strelov1/freehire/internal/identity/auth"
	"github.com/strelov1/freehire/internal/platform/db"
)

func TestWebhookEndToEnd(t *testing.T) {
	pool := startPostgres(t)
	ctx := context.Background()

	var userID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, email_verified) VALUES ('webhook@example.test', true) RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	iss := auth.NewIssuer("test-secret", time.Hour)
	cookie, _ := iss.Issue(userID, testTokenVersion)
	queries := db.New(pool)
	h := newWebhookHandlers(queries, testWebhookCipher(t))

	app := fiber.New(fiber.Config{ErrorHandler: RenderError})
	app.Post("/api/v1/me/webhook", auth.RequireAuth(iss, testVersions), h.CreateOrRotateWebhook)
	app.Get("/api/v1/me/webhook", auth.RequireAuth(iss, testVersions), h.GetWebhook)
	app.Patch("/api/v1/me/webhook", auth.RequireAuth(iss, testVersions), h.SetWebhookEnabled)
	app.Delete("/api/v1/me/webhook", auth.RequireAuth(iss, testVersions), h.DeleteWebhook)

	cookieReq := func(method, path string, body []byte) *http.Request {
		var r *http.Request
		if body != nil {
			r = httptest.NewRequest(method, path, bytes.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
		} else {
			r = httptest.NewRequest(method, path, nil)
		}
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: cookie})
		return r
	}

	// GET before any destination exists returns a null data field, not a 404.
	getResp, err := app.Test(cookieReq(fiber.MethodGet, "/api/v1/me/webhook", nil))
	if err != nil {
		t.Fatalf("get (unconfigured): %v", err)
	}
	var unconfigured struct {
		Data *struct{} `json:"data"`
	}
	if err := json.NewDecoder(getResp.Body).Decode(&unconfigured); err != nil {
		t.Fatalf("decode get (unconfigured): %v", err)
	}
	if unconfigured.Data != nil {
		t.Errorf("GET before creation: data = %+v, want null", unconfigured.Data)
	}

	// Create returns the secret exactly once.
	createResp, err := app.Test(cookieReq(fiber.MethodPost, "/api/v1/me/webhook",
		[]byte(`{"url":"https://example.test/hook"}`)))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if createResp.StatusCode != fiber.StatusCreated {
		body, _ := io.ReadAll(createResp.Body)
		t.Fatalf("create status = %d, want 201 (body %s)", createResp.StatusCode, body)
	}
	var created struct {
		Data struct {
			URL     string `json:"url"`
			Enabled bool   `json:"enabled"`
			Secret  string `json:"secret"`
		} `json:"data"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.Data.URL != "https://example.test/hook" || !created.Data.Enabled {
		t.Errorf("created = %+v, want the given URL and enabled=true", created.Data)
	}
	if created.Data.Secret == "" {
		t.Error("create response carries no secret")
	}
	firstSecret := created.Data.Secret

	// GET afterward never repeats the secret.
	getResp2, err := app.Test(cookieReq(fiber.MethodGet, "/api/v1/me/webhook", nil))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	getBody, _ := io.ReadAll(getResp2.Body)
	if bytes.Contains(getBody, []byte(firstSecret)) {
		t.Error("GET response leaks the secret")
	}

	// Disable, then re-enable without rotating.
	disableResp, err := app.Test(cookieReq(fiber.MethodPatch, "/api/v1/me/webhook", []byte(`{"enabled":false}`)))
	if err != nil {
		t.Fatalf("disable: %v", err)
	}
	var toggled struct {
		Data struct {
			URL     string `json:"url"`
			Enabled bool   `json:"enabled"`
		} `json:"data"`
	}
	if err := json.NewDecoder(disableResp.Body).Decode(&toggled); err != nil {
		t.Fatalf("decode disable: %v", err)
	}
	if toggled.Data.Enabled {
		t.Error("after PATCH enabled=false, want enabled=false")
	}
	if toggled.Data.URL != "https://example.test/hook" {
		t.Errorf("disable must not change the url, got %q", toggled.Data.URL)
	}

	enableResp, err := app.Test(cookieReq(fiber.MethodPatch, "/api/v1/me/webhook", []byte(`{"enabled":true}`)))
	if err != nil {
		t.Fatalf("enable: %v", err)
	}
	if err := json.NewDecoder(enableResp.Body).Decode(&toggled); err != nil {
		t.Fatalf("decode enable: %v", err)
	}
	if !toggled.Data.Enabled {
		t.Error("after PATCH enabled=true, want enabled=true")
	}

	// Rotating replaces the secret.
	rotateResp, err := app.Test(cookieReq(fiber.MethodPost, "/api/v1/me/webhook",
		[]byte(`{"url":"https://example.test/hook"}`)))
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	var rotated struct {
		Data struct {
			Secret string `json:"secret"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rotateResp.Body).Decode(&rotated); err != nil {
		t.Fatalf("decode rotate: %v", err)
	}
	if rotated.Data.Secret == "" || rotated.Data.Secret == firstSecret {
		t.Errorf("rotated secret = %q, want a new non-empty secret (old: %q)", rotated.Data.Secret, firstSecret)
	}

	// Delete removes it; a second delete 404s.
	deleteResp, err := app.Test(cookieReq(fiber.MethodDelete, "/api/v1/me/webhook", nil))
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if deleteResp.StatusCode != fiber.StatusNoContent {
		t.Errorf("delete status = %d, want 204", deleteResp.StatusCode)
	}
	deleteAgainResp, err := app.Test(cookieReq(fiber.MethodDelete, "/api/v1/me/webhook", nil))
	if err != nil {
		t.Fatalf("delete again: %v", err)
	}
	if deleteAgainResp.StatusCode != fiber.StatusNotFound {
		t.Errorf("second delete status = %d, want 404", deleteAgainResp.StatusCode)
	}

	// Every endpoint requires the session cookie.
	noCookie := httptest.NewRequest(fiber.MethodGet, "/api/v1/me/webhook", nil)
	noCookieResp, err := app.Test(noCookie)
	if err != nil {
		t.Fatalf("no-cookie get: %v", err)
	}
	if noCookieResp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("no-cookie get status = %d, want 401", noCookieResp.StatusCode)
	}
}

// RecordWebhookDeliverySuccess (called by notify.Runner's deliverOne on a
// successful webhook send) actually stamps last_success_at, and GetWebhook
// surfaces it.
func TestRecordWebhookDeliverySuccessStampsLastSuccessAt(t *testing.T) {
	pool := startPostgres(t)
	ctx := context.Background()

	var userID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, email_verified) VALUES ('webhook-success@example.test', true) RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	queries := db.New(pool)
	if _, err := queries.UpsertWebhookConfig(ctx, db.UpsertWebhookConfigParams{
		UserID: userID, URL: "https://example.test/hook", SecretEncrypted: "enc",
	}); err != nil {
		t.Fatalf("create webhook config: %v", err)
	}

	before, err := queries.GetWebhookConfig(ctx, userID)
	if err != nil {
		t.Fatalf("get before: %v", err)
	}
	if before.LastSuccessAt.Valid {
		t.Fatal("last_success_at should start unset")
	}

	if err := queries.RecordWebhookDeliverySuccess(ctx, userID); err != nil {
		t.Fatalf("record delivery success: %v", err)
	}

	after, err := queries.GetWebhookConfig(ctx, userID)
	if err != nil {
		t.Fatalf("get after: %v", err)
	}
	if !after.LastSuccessAt.Valid {
		t.Error("last_success_at should be set after RecordWebhookDeliverySuccess")
	}
}

// GetWebhook stays registered and reports "unconfigured" (not an error) even
// when the webhook feature is off server-side (cipher nil): the frontend's
// shared notification-state load calls it inside the same Promise.all as
// telegramStatus/listSubscriptions, so a route that failed here would fail
// that whole load — not just hide the webhook chip.
func TestWebhookGetServedWithoutCipher(t *testing.T) {
	pool := startPostgres(t)
	ctx := context.Background()

	var userID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, email_verified) VALUES ('webhook-nocipher@example.test', true) RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	iss := auth.NewIssuer("test-secret", time.Hour)
	cookie, _ := iss.Issue(userID, testTokenVersion)
	h := newWebhookHandlers(db.New(pool), nil) // nil cipher: feature off

	app := fiber.New(fiber.Config{ErrorHandler: RenderError})
	apiGroup := app.Group("/api/v1")
	h.register(apiGroup, middleware{cookie: auth.RequireAuth(iss, testVersions)})

	req := httptest.NewRequest(fiber.MethodGet, "/api/v1/me/webhook", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: cookie})
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want 200 (GET must be served even with no cipher)", resp.StatusCode)
	}
	var out struct {
		Data *struct{} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Data != nil {
		t.Errorf("data = %+v, want null", out.Data)
	}

	// Writes stay unregistered without a cipher.
	createReq := httptest.NewRequest(fiber.MethodPost, "/api/v1/me/webhook",
		bytes.NewReader([]byte(`{"url":"https://example.test/hook"}`)))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.AddCookie(&http.Cookie{Name: auth.CookieName, Value: cookie})
	createResp, err := app.Test(createReq)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if createResp.StatusCode != fiber.StatusMethodNotAllowed {
		t.Errorf("create status = %d, want 405 (writes must be unregistered with no cipher)", createResp.StatusCode)
	}
}
