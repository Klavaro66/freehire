// Package webhooknotify is the webhook implementation of notify.Notifier: it
// HMAC-signs a subscription digest and POSTs it to the account's configured
// webhook destination. Unlike the other channels, the destination is a URL the
// user supplies, so every send goes through an SSRF-guarded client
// (internal/platform/safehttp) and the response is watched for the one
// definitive "stop sending" signal a receiver can give us (HTTP 410).
package webhooknotify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/strelov1/freehire/internal/engage/notify"
	"github.com/strelov1/freehire/internal/platform/safehttp"
	"github.com/strelov1/freehire/internal/platform/tokencrypt"
)

// SignatureHeader carries the payload's HMAC-SHA256 signature, hex-encoded and
// prefixed "sha256=", so a receiver can verify the request came from freehire
// and was not altered in transit.
const SignatureHeader = "X-Freehire-Signature"

// requestTimeout bounds one delivery attempt. A slow or hanging third-party
// endpoint must not hold up the worker pass beyond a bounded window; the
// engine's own retry/dead-letter bookkeeping is what a timeout feeds into.
const requestTimeout = 10 * time.Second

// Compile-time guarantee that Notifier satisfies the channel abstraction.
var _ notify.Notifier = (*Notifier)(nil)

// Notifier delivers a digest as a signed HTTP POST to the destination encoded
// in dest (a notify.WebhookDest, see recipient() in internal/engage/notify).
type Notifier struct {
	cipher *tokencrypt.Cipher
	http   *http.Client
}

// NewNotifier builds a Notifier with an SSRF-guarded client bounded by
// requestTimeout.
func NewNotifier(cipher *tokencrypt.Cipher) *Notifier {
	return newNotifier(cipher, safehttp.NewClient(requestTimeout))
}

// newNotifier builds a Notifier against an arbitrary HTTP client. The seam
// exists for tests: safehttp refuses private addresses, which is correct in
// production and makes a loopback test server unreachable (see
// internal/identity/billing's client for the same pattern).
func newNotifier(cipher *tokencrypt.Cipher, httpc *http.Client) *Notifier {
	return &Notifier{cipher: cipher, http: httpc}
}

// payload is the JSON body POSTed to the destination.
type payload struct {
	SavedSearchName string             `json:"saved_search_name"`
	Total           int                `json:"total"`
	Jobs            []notify.DigestJob `json:"jobs"`
}

// Send unmarshals dest, decrypts its secret, signs the digest body, and POSTs
// it. A 410 Gone is translated to notify.ErrRecipientGone — the engine-side
// vocabulary for "this recipient will not accept messages again" — so the
// engine disables the destination and soft-skips instead of counting a
// delivery failure it would retry to no purpose.
func (n *Notifier) Send(ctx context.Context, _ string, dest string, d notify.Digest) error {
	var wd notify.WebhookDest
	if err := json.Unmarshal([]byte(dest), &wd); err != nil {
		return fmt.Errorf("webhooknotify: invalid dest %q: %w", dest, err)
	}
	if err := validateScheme(wd.URL); err != nil {
		return fmt.Errorf("webhooknotify: %w", err)
	}
	secret, err := n.cipher.Decrypt(wd.SecretEncrypted)
	if err != nil {
		return fmt.Errorf("webhooknotify: decrypt secret: %w", err)
	}

	body, err := json.Marshal(payload{SavedSearchName: d.SavedSearchName, Total: d.Total, Jobs: d.Jobs})
	if err != nil {
		return fmt.Errorf("webhooknotify: encode payload: %w", err)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, wd.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("webhooknotify: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(SignatureHeader, signature)

	resp, err := n.http.Do(req)
	if err != nil {
		return fmt.Errorf("webhooknotify: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusGone {
		return fmt.Errorf("%w: webhook destination responded 410 Gone", notify.ErrRecipientGone)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhooknotify: destination responded %d", resp.StatusCode)
	}
	return nil
}

// validateScheme rejects anything but http/https before a request is ever
// built — defense in depth alongside the API layer's own creation-time check
// (see internal/api/handler), and the only thing standing between a malformed
// dest and an attempted send since recipient() does not otherwise validate it.
func validateScheme(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid destination url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported destination url scheme %q", u.Scheme)
	}
	return nil
}
