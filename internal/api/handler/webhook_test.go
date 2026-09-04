package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/strelov1/freehire/internal/identity/auth"
	"github.com/strelov1/freehire/internal/platform/tokencrypt"
)

func testWebhookCipher(t *testing.T) *tokencrypt.Cipher {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	c, err := tokencrypt.New(key)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// webhookConfigApp mounts the webhook-management routes behind RequireAuth (cookie-only)
// on a handler with no DB. The cookie-gate cases below reject before any query
// runs, so the nil queries is never dereferenced. The DB-backed paths are covered
// by the webhook integration tests.
func webhookConfigApp(t *testing.T) *fiber.App {
	t.Helper()
	iss := auth.NewIssuer("test-secret", time.Hour)
	h := newWebhookHandlers(nil, testWebhookCipher(t))
	app := fiber.New()
	app.Post("/api/v1/me/webhook", auth.RequireAuth(iss, testVersions), h.CreateOrRotateWebhook)
	app.Get("/api/v1/me/webhook", auth.RequireAuth(iss, testVersions), h.GetWebhook)
	app.Patch("/api/v1/me/webhook", auth.RequireAuth(iss, testVersions), h.SetWebhookEnabled)
	app.Delete("/api/v1/me/webhook", auth.RequireAuth(iss, testVersions), h.DeleteWebhook)
	return app
}

// Webhook management is cookie-only: a request with an API key (or nothing) but
// no session cookie must be rejected, matching subscription management — the
// destination's secret is itself a credential.
func TestWebhookManagement_IsCookieOnly(t *testing.T) {
	app := webhookConfigApp(t)
	cases := []struct {
		name, method, path string
		bearer             bool
	}{
		{"create, no credential", fiber.MethodPost, "/api/v1/me/webhook", false},
		{"create, bearer only", fiber.MethodPost, "/api/v1/me/webhook", true},
		{"get, bearer only", fiber.MethodGet, "/api/v1/me/webhook", true},
		{"patch, bearer only", fiber.MethodPatch, "/api/v1/me/webhook", true},
		{"delete, bearer only", fiber.MethodDelete, "/api/v1/me/webhook", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(context.Background(), tc.method, tc.path, nil)
			if tc.bearer {
				req.Header.Set("Authorization", "Bearer fhk_whatever")
			}
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("Test: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != fiber.StatusUnauthorized {
				t.Errorf("status = %d, want 401 (webhook management must be cookie-only)", resp.StatusCode)
			}
		})
	}
}

// A non-http(s) URL is rejected before any query runs (nil queries would panic
// if reached), so this needs no DB.
func TestCreateOrRotateWebhook_RejectsNonHTTPScheme(t *testing.T) {
	iss := auth.NewIssuer("test-secret", time.Hour)
	h := newWebhookHandlers(nil, testWebhookCipher(t))
	app := fiber.New()
	app.Post("/api/v1/me/webhook", auth.RequireAuth(iss, testVersions), h.CreateOrRotateWebhook)

	token, err := iss.Issue(1, testTokenVersion)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"url": "ftp://example.com/hook"})
	req := httptest.NewRequestWithContext(context.Background(), fiber.MethodPost, "/api/v1/me/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want 400 for a non-http(s) url", resp.StatusCode)
	}
}

// The created-webhook response carries the plaintext secret exactly once,
// alongside the destination metadata.
func TestCreatedWebhookResponse_IncludesSecret(t *testing.T) {
	fields := marshalToFields(t, createdWebhookResponse{
		webhookResponse: webhookResponse{URL: "https://example.com/hook", Enabled: true},
		Secret:          "the-one-time-secret",
	})
	for _, want := range []string{"url", "enabled", "created_at", "last_success_at", "disabled_at", "secret"} {
		if _, ok := fields[want]; !ok {
			t.Errorf("created-webhook response missing %q", want)
		}
	}
}

// Write endpoints stay unregistered when the webhook feature is off
// server-side, unlike GetWebhook above.
func TestWebhookRegister_WritesAreUnregisteredWithoutCipher(t *testing.T) {
	iss := auth.NewIssuer("test-secret", time.Hour)
	h := newWebhookHandlers(nil, nil)
	app := fiber.New(fiber.Config{ErrorHandler: RenderError})
	api := app.Group("/api/v1")
	h.register(api, middleware{cookie: auth.RequireAuth(iss, testVersions)})

	token, err := iss.Issue(1, testTokenVersion)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequestWithContext(context.Background(), fiber.MethodPost, "/api/v1/me/webhook",
		bytes.NewReader([]byte(`{"url":"https://example.com/hook"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	defer resp.Body.Close()
	// 405, not 404: GET on the same path is registered (see the test above), so
	// Fiber recognizes the path and rejects the method rather than the route.
	if resp.StatusCode != fiber.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405 (create must be unregistered with no cipher)", resp.StatusCode)
	}
}

// The metadata response never exposes the secret.
func TestWebhookResponse_OmitsSecret(t *testing.T) {
	fields := marshalToFields(t, webhookResponse{URL: "https://example.com/hook", Enabled: true})
	if _, leaked := fields["secret"]; leaked {
		t.Error("metadata response must not include the secret")
	}
	for _, want := range []string{"url", "enabled", "created_at", "last_success_at", "disabled_at"} {
		if _, ok := fields[want]; !ok {
			t.Errorf("metadata response missing %q", want)
		}
	}
}
