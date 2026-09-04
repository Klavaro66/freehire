package webhooknotify

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/strelov1/freehire/internal/engage/notify"
	"github.com/strelov1/freehire/internal/platform/tokencrypt"
)

func testCipher(t *testing.T) *tokencrypt.Cipher {
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

// testDest builds a recipient() dest string the way notify.recipient() would,
// encrypting secret with cipher.
func testDest(t *testing.T, cipher *tokencrypt.Cipher, url, secret string) string {
	t.Helper()
	enc, err := cipher.Encrypt(secret)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(notify.WebhookDest{URL: url, SecretEncrypted: enc})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSend_SignsBodyWithHMACSHA256(t *testing.T) {
	cipher := testCipher(t)
	var gotBody []byte
	var gotSig, gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotSig = r.Header.Get(SignatureHeader)
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// The server's own client, not safehttp's: safehttp refuses private
	// addresses, which is exactly right in production and exactly wrong for a
	// loopback test server (see internal/identity/billing's client for the
	// same seam).
	n := newNotifier(cipher, srv.Client())
	d := notify.Digest{SavedSearchName: "Go jobs", Total: 1, Jobs: []notify.DigestJob{{Title: "Gopher", Slug: "gopher"}}}
	dest := testDest(t, cipher, srv.URL, "top-secret")

	if err := n.Send(context.Background(), notify.ChannelWebhook, dest, d); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	mac := hmac.New(sha256.New, []byte("top-secret"))
	mac.Write(gotBody)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if gotSig != want {
		t.Errorf("signature header = %q, want %q", gotSig, want)
	}
}

func TestSend_RotatedSecretChangesTheSignature(t *testing.T) {
	cipher := testCipher(t)
	var gotBody []byte
	var gotSig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotSig = r.Header.Get(SignatureHeader)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := newNotifier(cipher, srv.Client())
	dest := testDest(t, cipher, srv.URL, "new-secret")
	if err := n.Send(context.Background(), notify.ChannelWebhook, dest, notify.Digest{}); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	oldMac := hmac.New(sha256.New, []byte("old-secret"))
	oldMac.Write(gotBody)
	oldSig := "sha256=" + hex.EncodeToString(oldMac.Sum(nil))
	if gotSig == oldSig {
		t.Error("signature computed with the old secret should not match the rotated secret's signature")
	}
}

func TestSend_410IsRecipientGone(t *testing.T) {
	cipher := testCipher(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusGone)
	}))
	defer srv.Close()

	n := newNotifier(cipher, srv.Client())
	dest := testDest(t, cipher, srv.URL, "secret")
	err := n.Send(context.Background(), notify.ChannelWebhook, dest, notify.Digest{})
	if !errors.Is(err, notify.ErrRecipientGone) {
		t.Errorf("Send error = %v, want wrapping notify.ErrRecipientGone", err)
	}
}

func TestSend_ServerErrorIsPlainError(t *testing.T) {
	cipher := testCipher(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	n := newNotifier(cipher, srv.Client())
	dest := testDest(t, cipher, srv.URL, "secret")
	err := n.Send(context.Background(), notify.ChannelWebhook, dest, notify.Digest{})
	if err == nil {
		t.Fatal("Send: want an error for a 500 response")
	}
	if errors.Is(err, notify.ErrRecipientGone) {
		t.Error("Send: a 500 must follow the normal retry/dead-letter path, not ErrRecipientGone")
	}
}

func TestSend_SuccessStampsNoErrorOnAny2xx(t *testing.T) {
	cipher := testCipher(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	n := newNotifier(cipher, srv.Client())
	dest := testDest(t, cipher, srv.URL, "secret")
	if err := n.Send(context.Background(), notify.ChannelWebhook, dest, notify.Digest{}); err != nil {
		t.Errorf("Send returned error for a 202: %v", err)
	}
}

func TestSend_RejectsNonHTTPScheme(t *testing.T) {
	cipher := testCipher(t)
	n := newNotifier(cipher, http.DefaultClient)
	dest := testDest(t, cipher, "ftp://example.com/hook", "secret")
	if err := n.Send(context.Background(), notify.ChannelWebhook, dest, notify.Digest{}); err == nil {
		t.Error("Send: want an error for a non-http(s) URL, got none")
	}
}

func TestSend_InvalidDestIsError(t *testing.T) {
	cipher := testCipher(t)
	n := newNotifier(cipher, http.DefaultClient)
	if err := n.Send(context.Background(), notify.ChannelWebhook, "not json", notify.Digest{}); err == nil {
		t.Error("Send: want an error for a malformed dest, got none")
	}
}

// NewNotifier's production client is SSRF-guarded (internal/platform/safehttp):
// pointed at a loopback address, the send must be refused rather than delivered.
func TestSend_ProductionClientRejectsPrivateAddress(t *testing.T) {
	cipher := testCipher(t)
	n := NewNotifier(cipher)
	dest := testDest(t, cipher, "http://127.0.0.1:1/hook", "secret")
	if err := n.Send(context.Background(), notify.ChannelWebhook, dest, notify.Digest{}); err == nil {
		t.Error("Send: want an error when the production client targets a loopback address")
	}
}
